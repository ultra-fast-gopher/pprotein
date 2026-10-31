<template>
  <TsvTable
    :key="requestUrl"
    :tsv="tsv"
    :link="`/api/${apiEndpoint}/data/${$route.params.id}`"
    :initial-sort-column="apiEndpoint === 'pgslowlog' ? 4 : 0"
  />
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TsvTable from "./TsvTable.vue";

export default defineComponent({
  components: {
    TsvTable,
  },
  data() {
    return {
      tsv: "",
    };
  },
  computed: {
    apiEndpoint(): string {
      return this.$route.meta.logEndpoint === "pgslowlog"
        ? "pgslowlog"
        : "slowlog";
    },
    requestUrl(): string {
      return `/api/${this.apiEndpoint}/${this.$route.params.id}`;
    },
  },
  watch: {
    requestUrl: {
      immediate: true,
      async handler(url: string) {
        this.tsv = "";
        const resp = await fetch(url);
        const tsv = await resp.text();
        if (url === this.requestUrl) {
          this.tsv = tsv;
        }
      },
    },
  },
});
</script>
