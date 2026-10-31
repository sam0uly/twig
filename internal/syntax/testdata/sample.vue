<template>
  <div :class="wrapClass" @click="toggle">
    <span v-if="count > 0">{{ count }} items</span>
    <ul>
      <li v-for="item in items" :key="item.id">{{ item.name }}</li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";

const count = ref(0);
const items = ref<{ id: number; name: string }[]>([]);

function toggle(): void {
  count.value += 1;
}
</script>

<style scoped lang="scss">
.wrap {
  display: flex;
  color: red;
}
</style>
