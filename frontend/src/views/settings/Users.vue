<template>
  <errors v-if="error" :errorCode="error.status" />

  <div v-else class="flex flex-col gap-4">
    <div class="flex justify-end">
      <router-link
        to="/settings/users/new"
        class="btn btn-flex btn-blue btn-soft"
      >
        <i class="fa-solid fa-user-plus"></i>
        <span>{{ t("buttons.new") }}</span>
      </router-link>
    </div>

    <Table>
      <Thead>
        <Th>{{ t("settings.username") }}</Th>
        <Th>{{ t("settings.admin") }}</Th>
        <Th>{{ t("settings.scope") }}</Th>
        <Th align="right"></Th>
      </Thead>

      <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
        <Tr v-for="user in users" :key="user.id">
          <Td>
            <router-link
              :to="'/settings/users/' + user.id"
              class="font-medium text-blue-600 dark:text-gray-100 hover:underline"
              >{{ user.username }}</router-link
            >
          </Td>
          <Td>
            <StatusBadge
              :status="user.perm.admin ? 'success' : 'default'"
              :display="user.perm.admin ? t('buttons.yes') : t('buttons.no')"
            />
          </Td>
          <Td>
            <code class="font-mono text-xs">{{ user.scope }}</code>
          </Td>
          <Td align="right">
            <IconAction
              :href="'/settings/users/' + user.id"
              icon="fa-pen-to-square"
              :title="t('buttons.edit')"
              class="inline-flex"
            />
          </Td>
        </Tr>

        <Tr v-if="!users.length" :hover="false">
          <Td colspan="4">
            <div
              class="flex flex-col items-center gap-2 py-6 text-gray-600 dark:text-gray-300"
            >
              <i class="fa-solid fa-users text-3xl"></i>
              <span class="text-sm font-medium">{{
                t("settings.userManagement")
              }}</span>
            </div>
          </Td>
        </Tr>
      </tbody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useLayoutStore } from "@/stores/layout";
import { users as api } from "@/api";
import { StatusError } from "@/api/utils";

import Errors from "@/views/Errors.vue";
import Table from "@/components/ui/Table.vue";
import Thead from "@/components/ui/Thead.vue";
import Th from "@/components/ui/Th.vue";
import Tr from "@/components/ui/Tr.vue";
import Td from "@/components/ui/Td.vue";
import StatusBadge from "@/components/ui/StatusBadge.vue";
import IconAction from "@/components/ui/IconAction.vue";

const error = ref<StatusError | null>(null);
const users = ref<IUser[]>([]);

const layoutStore = useLayoutStore();
const { t } = useI18n();

onMounted(async () => {
  layoutStore.loading = true;

  try {
    users.value = await api.getAll();
  } catch (err) {
    if (err instanceof Error) {
      error.value = err;
    }
  } finally {
    layoutStore.loading = false;
  }
});
</script>
