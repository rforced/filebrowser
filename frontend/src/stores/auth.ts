import { defineStore } from "pinia";
import { detectLocale, setLocale } from "@/i18n";
import { cloneDeep } from "lodash-es";

export const useAuthStore = defineStore("auth", {
  // convert to a function
  state: (): {
    user: IUser | null;
    token: string;
  } => ({
    user: null,
    token: "",
  }),
  getters: {
    // user and token getter removed, no longer needed
    isLoggedIn: (state) => state.user !== null,
  },
  actions: {
    // no context as first argument, use `this` instead
    setUser(user: IUser) {
      if (user === null) {
        this.user = null;
        return;
      }

      setLocale(user.locale || detectLocale());
      this.user = user;
    },
    updateUser(user: Partial<IUser>) {
      if (user.locale) {
        setLocale(user.locale);
      }

      this.user = { ...this.user, ...cloneDeep(user) } as IUser;
    },
    // easily reset state using `$reset`
    clearUser() {
      this.$reset();
    },
  },
});
