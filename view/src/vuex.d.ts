import store from "./store";

declare module "vue" {
  interface ComponentCustomProperties {
    $store: typeof store;
  }
}
