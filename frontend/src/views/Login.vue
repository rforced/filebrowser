<template>
  <div id="login">
    <form @submit="submit">
      <img :src="logoURL" alt="File Browser" />
      <h1>{{ name }}</h1>
      <p v-if="reason != null" class="logout-message">
        {{ t(`login.logout_reasons.${reason}`) }}
      </p>
      <div v-if="error !== ''" class="wrong">{{ error }}</div>

      <input
        autofocus
        class="input input--block"
        type="text"
        autocapitalize="off"
        v-model="username"
        :placeholder="t('login.username')"
      />
      <input
        class="input input--block"
        type="password"
        v-model="password"
        :placeholder="t('login.password')"
      />
      <input
        class="button button--block"
        type="submit"
        :value="t('login.submit')"
        :disabled="loading"
      />
    </form>
    <span v-if="version" class="version">{{ version }}</span>
  </div>
</template>

<script setup lang="ts">
import { StatusError } from "@/api/utils";
import * as auth from "@/utils/auth";
import {
  name,
  logoURL,
  recaptcha,
  recaptchaKey,
  version,
} from "@/utils/constants";
import { inject, ref, onMounted, onBeforeUnmount } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

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
