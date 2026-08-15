<template>
  <td
    class="px-3 py-3 first:pl-4 last:pr-4 text-sm"
    :class="[
      align === 'right' ? 'text-right' : '',
      truncate ? 'whitespace-nowrap overflow-hidden text-ellipsis' : '',
    ]"
  >
    <slot />
  </td>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ align?: "left" | "right"; truncate?: boolean }>(), {
  align: "left",
  truncate: true,
});
</script>
