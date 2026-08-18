import { createApp } from "vue";
import VueNumberInput from "@chenfengyuan/vue-number-input";
import VueLazyload from "vue-lazyload";
import createPinia from "@/stores";
import router from "@/router";
import i18n from "@/i18n";
import App from "@/App.vue";
import { useToastStore } from "@/stores/toast";
import { tooltip } from "@/utils/tooltip";
import { embedded } from "@/utils/embedded";

import dayjs from "dayjs";
import localizedFormat from "dayjs/plugin/localizedFormat";
import relativeTime from "dayjs/plugin/relativeTime";
import duration from "dayjs/plugin/duration";

import "./css/app.css";

dayjs.extend(localizedFormat);
dayjs.extend(relativeTime);
dayjs.extend(duration);

// CSS hook for the slimmed-down chrome inside the platform's iframe.
document.documentElement.classList.toggle("embedded", embedded);

const pinia = createPinia(router);

const app = createApp(App);

app.component(VueNumberInput.name || "vue-number-input", VueNumberInput);
app.use(VueLazyload);

app.use(i18n);
app.use(pinia);
app.use(router);

app.mixin({
  mounted() {
    // expose vue instance to components
    this.$el.__vue__ = this;
  },
});

// provide v-focus for components
app.directive("focus", {
  mounted: async (el) => {
    // initiate focus for the element
    el.focus();
  },
});

// Horizon's x-tooltip equivalent; see utils/tooltip.ts.
app.directive("tooltip", tooltip);

const toastStore = useToastStore(pinia);

app.provide("$showSuccess", (message: string) => {
  toastStore.show(message, "success");
});

app.provide("$showError", (error: Error | string) => {
  const message = error instanceof Error ? error.message : String(error);
  toastStore.show(message, "error", 0);
});

router.isReady().then(() => app.mount("#app"));
