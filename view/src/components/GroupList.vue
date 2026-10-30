<template>
  <section>
    <div class="form">
      <label>
        Webhook URL<br />
        <input v-model="url" type="text" size="50" disabled />
      </label>
      <label>
        &nbsp;<br />
        <button @click="collect">Collect</button>
      </label>
      <label>
        &nbsp;<br />
        <button @click="startRun" :disabled="busy || hasActiveRun">
          Start session
        </button>
      </label>
      <label>
        &nbsp;<br />
        <button @click="stopRun" :disabled="busy || !canStopRun">
          End session
        </button>
      </label>
      <label>
        &nbsp;<br />
        <button @click="openAll = !openAll">
          {{ openAll ? "Close" : "Open" }} all
        </button>
      </label>
    </div>
    <div v-if="latestRun" class="run-status">
      <strong>Session {{ latestRun.ID }}: {{ latestRun.Status }}</strong>
      <span v-for="target in latestRun.Targets" :key="`${target.Type}-${target.Label}`">
        {{ target.Label }} ({{ target.Type }}): {{ target.Status }}<span v-if="target.Error"> — {{ target.Error }}</span>
      </span>
    </div>
    <div v-if="actionError" class="error">{{ actionError }}</div>
    <details
      v-for="(group, i) in $store.state.groups"
      :key="group"
      :open="i === 0 || openAll"
    >
      <summary>Group: {{ group }}</summary>
      <GroupEntriesTable
        :group-id="group"
        :entries="$store.getters.entriesByGroup(group)"
      />
      <AddMemo :group-id="group" />
    </details>
    <div v-if="!$store.state.groups.length">No entries!!</div>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import GroupEntriesTable from "./GroupEntriesTable.vue";
import AddMemo from "./AddMemo.vue";

interface RunTarget {
  Type: string;
  Label: string;
  Status: string;
  Error: string;
  SnapshotID: string;
}

interface SessionRun {
  ID: string;
  Status: string;
  Targets: RunTarget[];
}

export default defineComponent({
  components: {
    GroupEntriesTable,
    AddMemo,
  },
  data() {
    return {
      openAll: false,
      runs: [] as SessionRun[],
      busy: false,
      actionError: "",
      pollTimer: undefined as number | undefined,
    };
  },
  created() {
    void this.refreshRuns();
    this.pollTimer = window.setInterval(() => void this.refreshRuns(), 1500);
  },
  beforeUnmount() {
    if (this.pollTimer !== undefined) {
      window.clearInterval(this.pollTimer);
    }
  },
  computed: {
    url() {
      return `${location.origin}/api/group/collect`;
    },
    latestRun(): SessionRun | null {
      return this.runs[0] || null;
    },
    hasActiveRun(): boolean {
      return this.runs.some((run) =>
        ["starting", "running", "stopping", "interrupted"].includes(run.Status)
      );
    },
    canStopRun(): boolean {
      return !!this.latestRun && ["running", "interrupted"].includes(this.latestRun.Status);
    },
  },
  methods: {
    async collect() {
      const resp = await fetch(this.url);
      if (!resp.ok) {
        return alert(
          `http error: status=${resp.status}, message=${await resp.text()}`
        );
      }
    },
    async refreshRuns() {
      try {
        const resp = await fetch("/api/runs");
        if (resp.ok) {
          this.runs = (await resp.json()) as SessionRun[];
        }
      } catch (_) {
        // The existing group view remains usable if session polling is unavailable.
      }
    },
    async startRun() {
      this.busy = true;
      this.actionError = "";
      try {
        const resp = await fetch("/api/runs", { method: "POST" });
        if (!resp.ok) {
          this.actionError = await resp.text();
        }
        await this.refreshRuns();
      } catch (error) {
        this.actionError = error instanceof Error ? error.message : String(error);
      } finally {
        this.busy = false;
      }
    },
    async stopRun() {
      if (!this.latestRun) return;
      this.busy = true;
      this.actionError = "";
      try {
        const resp = await fetch(`/api/runs/${encodeURIComponent(this.latestRun.ID)}/stop`, {
          method: "POST",
        });
        if (!resp.ok) {
          this.actionError = await resp.text();
        }
        await this.refreshRuns();
      } catch (error) {
        this.actionError = error instanceof Error ? error.message : String(error);
      } finally {
        this.busy = false;
      }
    },
  },
});
</script>

<style scoped lang="scss">
section {
  margin: 2em;
}

details {
  margin-bottom: 1em;
}

.form {
  display: flex;
  margin-bottom: 2em;

  label {
    margin-right: 1em;

    &:focus-within {
      color: orangered;
    }

    input {
      border: 1px solid lightgray;
      padding: 0.4em 1em;

      &:focus {
        border-color: orangered;
        outline: 0;
      }
    }
  }
}

.run-status {
  display: flex;
  flex-direction: column;
  gap: 0.25em;
  margin: 0 0 1.5em;
}

.error {
  color: #a00;
  margin-bottom: 1em;
}
</style>
