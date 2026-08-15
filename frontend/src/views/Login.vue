<template>
  <div
    class="min-h-screen flex flex-col items-center justify-center gap-6 bg-gray-50 dark:bg-gray-900 p-4"
  >
    <Card class="w-full max-w-sm">
      <form class="flex flex-col gap-4 p-6 sm:p-8" @submit="submit">
        <div class="flex flex-col items-center gap-3">
          <h1
            class="text-2xl font-semibold text-blue-600 dark:text-gray-100 text-center break-words"
          >
            {{ name }}
          </h1>
        </div>

        <div
          v-if="reason != null"
          class="flex gap-2 items-start rounded-md bg-blue-50 dark:bg-gray-900 px-3 py-2 text-sm text-blue-700 dark:text-gray-300"
        >
          <i
            class="fa-solid fa-circle-info mt-0.5 text-blue-400 dark:text-teal"
          ></i>
          <span>{{ t(`login.logout_reasons.${reason}`) }}</span>
        </div>

        <div
          v-if="error !== ''"
          class="flex gap-2 items-start rounded-md bg-red-50 dark:bg-red-900/40 px-3 py-2 text-sm text-red-700 dark:text-red-200"
          role="alert"
        >
          <i class="fa-solid fa-circle-exclamation mt-0.5"></i>
          <span>{{ error }}</span>
        </div>

        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-1">
            <label for="login-username" class="form-label">{{
              t("login.username")
            }}</label>
            <input
              id="login-username"
              v-model="username"
              autofocus
              class="form-control"
              type="text"
              autocapitalize="off"
              autocomplete="username"
              :placeholder="t('login.username')"
            />
          </div>

          <div class="flex flex-col gap-1">
            <label for="login-password" class="form-label">{{
              t("login.password")
            }}</label>
            <input
              id="login-password"
              v-model="password"
              class="form-control"
              type="password"
              autocomplete="current-password"
              :placeholder="t('login.password')"
            />
          </div>
        </div>

        <button
          type="submit"
          class="btn btn-flex btn-blue w-full"
          :disabled="loading"
        >
          <i v-if="loading" class="fa-solid fa-spinner fa-spin"></i>
          <span>{{ t("login.submit") }}</span>
        </button>
      </form>
    </Card>

    <div class="flex flex-col items-center gap-3">
      <theme-switch />
      <span v-if="version" class="text-xs text-gray-500 dark:text-gray-400">{{
        version
      }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { StatusError } from "@/api/utils";
import * as auth from "@/utils/auth";
import {
  cspNonce,
  name,
  recaptcha,
  recaptchaKey,
  version,
} from "@/utils/constants";
import { inject, ref, onMounted, onBeforeUnmount } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import Card from "@/components/ui/Card.vue";
import ThemeSwitch from "@/components/ui/ThemeSwitch.vue";

// Define refs
const error = ref<string>("");
const username = ref<string>("");
const password = ref<string>("");
const loading = ref<boolean>(false);

const route = useRoute();
const router = useRouter();
const { t } = useI18n({});
// Define functions

const $showError = inject<IToastError>("$showError")!;

const reason = route.query["logout-reason"] ?? null;

// Dynamically load reCAPTCHA Enterprise script only on the login page
let recaptchaScript: HTMLScriptElement | null = null;

onMounted(() => {
  if (recaptcha && recaptchaKey) {
    recaptchaScript = document.createElement("script");
    if (cspNonce) {
      recaptchaScript.nonce = cspNonce;
      recaptchaScript.setAttribute("nonce", cspNonce);
    }
    recaptchaScript.src =
      "https://www.google.com/recaptcha/enterprise.js?render=" + recaptchaKey;
    document.head.appendChild(recaptchaScript);
  }
});

onBeforeUnmount(() => {
  // Remove the reCAPTCHA script tag
  if (recaptchaScript) {
    recaptchaScript.remove();
    recaptchaScript = null;
  }
  // Remove the reCAPTCHA badge injected by Google
  const badge = document.querySelector(".grecaptcha-badge");
  if (badge) {
    badge.remove();
  }
});

const submit = async (event: Event) => {
  event.preventDefault();
  event.stopPropagation();

  loading.value = true;
  error.value = "";

  const redirect = (route.query.redirect || "/files/") as string;

  let captcha = "";
  if (recaptcha) {
    try {
      // Wait for the reCAPTCHA Enterprise script to be ready
      await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(
          () => reject(new Error("reCAPTCHA script load timeout")),
          10000
        );
        const check = () => {
          if (
            typeof window.grecaptcha !== "undefined" &&
            typeof window.grecaptcha.enterprise !== "undefined"
          ) {
            clearTimeout(timeout);
            resolve();
          } else {
            setTimeout(check, 100);
          }
        };
        check();
      });

      captcha = await window.grecaptcha.enterprise.execute(recaptchaKey, {
        action: "login",
      });
    } catch {
      error.value = t("login.wrongCredentials");
      loading.value = false;
      return;
    }

    if (captcha === "") {
      error.value = t("login.wrongCredentials");
      loading.value = false;
      return;
    }
  }

  try {
    await auth.login(username.value, password.value, captcha);
    router.push({ path: redirect });
  } catch (e: any) {
    loading.value = false;
    if (e instanceof StatusError) {
      if (e.status === 429 && e.retryAfter) {
        error.value = t("login.rateLimited");
      } else if (e.status === 429) {
        error.value = t("login.captchaFailed");
      } else if (e.status === 403) {
        error.value = t("login.wrongCredentials");
      } else if (e.status === 400) {
        const match = e.message.match(/minimum length is (\d+)/);
        if (match) {
          error.value = t("login.passwordTooShort", { min: match[1] });
        } else {
          error.value = e.message;
        }
      } else {
        $showError(e);
      }
    }
  }
};
</script>
