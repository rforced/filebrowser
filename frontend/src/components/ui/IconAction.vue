<template>
  <component
    :is="href ? 'a' : 'button'"
    v-tooltip="title"
    :type="href ? undefined : 'button'"
    :href="href"
    :target="href && external ? '_blank' : undefined"
    :rel="href && external ? 'noopener noreferrer' : undefined"
    :disabled="href ? undefined : !enabled"
    :aria-label="title"
    class="flex items-center justify-center rounded-md transition-all shrink-0"
    :class="[
      SIZES[size],
      enabled
        ? ['text-gray-600 dark:text-gray-300', hoverColor]
        : 'text-gray-400 dark:text-gray-500 opacity-50 cursor-not-allowed',
    ]"
    @click="onClick"
  >
    <i :class="`fa-solid ${icon}`"></i>
  </component>
</template>

<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    icon: string;
    title: string;
    href?: string;
    enabled?: boolean;
    external?: boolean;
    size?: "sm" | "md" | "lg";
    hoverColor?: string;
  }>(),
  {
    enabled: true,
    external: false,
    size: "md",
    hoverColor:
      "hover:text-gray-900 dark:hover:text-white hover:bg-gray-200 dark:hover:bg-gray-600",
  }
);

const emit = defineEmits<{ (e: "action", event: MouseEvent): void }>();

const SIZES = {
  sm: "w-7 h-7 text-sm",
  md: "w-8 h-8",
  lg: "w-11 h-11 text-lg",
};

const onClick = (event: MouseEvent) => {
  if (!props.enabled) {
    // Anchors ignore the `disabled` attribute, so guard here too.
    event.preventDefault();
    return;
  }
  emit("action", event);
};
</script>
