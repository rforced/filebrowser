<template>
  <div class="grid md:flex gap-4">
    <div class="flex-1 min-w-0 flex flex-col gap-4">
      <slot name="main" />
    </div>

    <div class="w-full md:w-80 shrink-0 flex flex-col gap-4">
      <slot name="sidebar" />
    </div>
  </div>
</template>

<script setup lang="ts"></script>
