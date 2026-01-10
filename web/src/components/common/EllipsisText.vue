<template>
    <el-tooltip v-if="isOverflow" :content="text" placement="top">
        <span class="ellipsis-text prjh" ref="textRef" :style="{ maxWidth: maxWidth }">{{ text }}</span>
    </el-tooltip>
    <span v-else class="ellipsis-text prjh" ref="textRef" :style="{ maxWidth: maxWidth }">{{ text }}</span>
</template>

<script setup lang="ts">
import { ref, onMounted, watch, nextTick } from 'vue';

const props = defineProps({
    text: {
        type: String,
        required: true,
    },
    maxWidth: {
        type: String,
        default: '',
    },
});

const isOverflow = ref(false);
const textRef = ref<HTMLElement | null>(null);

const checkOverflow = () => {
    if (textRef.value) {
        isOverflow.value = textRef.value.scrollWidth > textRef.value.offsetWidth;
    }
};

onMounted(() => {
    nextTick(() => {
        checkOverflow();
    });
});

watch(() => props.text, () => {
    nextTick(() => {
        checkOverflow();
    });
});
</script>

<style scoped lang="scss">
.ellipsis-text {
    display: inline-block;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: middle;
}
</style>