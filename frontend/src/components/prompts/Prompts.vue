<template>
  <Modal
    v-if="modal != null"
    :size="size"
    :close-button="false"
    @closed="close"
  >
    <keep-alive>
      <component :is="modal" />
    </keep-alive>
  </Modal>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { storeToRefs } from "pinia";
import { useLayoutStore } from "@/stores/layout";

import Modal from "@/components/ui/Modal.vue";
import Help from "./Help.vue";
import Info from "./Info.vue";
import Delete from "./Delete.vue";
import DeleteUser from "./DeleteUser.vue";
import Download from "./Download.vue";
import Rename from "./Rename.vue";
import Move from "./Move.vue";
import Copy from "./Copy.vue";
import NewFile from "./NewFile.vue";
import NewDir from "./NewDir.vue";
import Replace from "./Replace.vue";
import Share from "./Share.vue";
import ShareDelete from "./ShareDelete.vue";
import Upload from "./Upload.vue";
import DiscardEditorChanges from "./DiscardEditorChanges.vue";
import ResolveConflict from "./ResolveConflict.vue";
import CurrentPassword from "./CurrentPassword.vue";
import Extract from "./Extract.vue";
import ConvergeClean from "./ConvergeClean.vue";
import ConvergeCombine from "./ConvergeCombine.vue";

const layoutStore = useLayoutStore();

const { currentPromptName } = storeToRefs(layoutStore);

const components = new Map<string, any>([
  ["info", Info],
  ["help", Help],
  ["delete", Delete],
  ["rename", Rename],
  ["move", Move],
  ["copy", Copy],
  ["newFile", NewFile],
  ["newDir", NewDir],
  ["download", Download],
  ["replace", Replace],
  ["share", Share],
  ["upload", Upload],
  ["share-delete", ShareDelete],
  ["deleteUser", DeleteUser],
  ["discardEditorChanges", DiscardEditorChanges],
  ["resolve-conflict", ResolveConflict],
  ["current-password", CurrentPassword],
  ["extract", Extract],
  ["converge-clean", ConvergeClean],
  ["converge-combine", ConvergeCombine],
]);

/*
 * Prompts that show a file tree, a directory picker or a conflict list need
 * more width than a confirmation dialog. Anything unlisted gets the default.
 */
const WIDE_PROMPTS = new Set([
  "move",
  "copy",
  "info",
  "help",
  "resolve-conflict",
  "converge-clean",
  "share",
]);

const modal = computed(() => components.get(currentPromptName.value!));

const size = computed(() =>
  WIDE_PROMPTS.has(currentPromptName.value ?? "") ? "lg" : "md"
);

const close = () => {
  if (!layoutStore.currentPrompt) return;
  layoutStore.closeHovers();
};
</script>
