<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount, nextTick, shallowRef, markRaw } from 'vue'
import { renderAsync } from 'docx-preview'
import { GlobalWorkerOptions, getDocument } from 'pdfjs-dist/legacy/build/pdf'
import PdfWorker from 'pdfjs-dist/legacy/build/pdf.worker?url'

type PreviewMode = 'text' | 'pdf' | 'word'

GlobalWorkerOptions.workerSrc = PdfWorker

const props = defineProps<{
  modelValue: boolean
  title: string
  mode: PreviewMode
  content: string
  blob: Blob | null
  fileName: string
  loading: boolean
  saving: boolean
  dirty: boolean
  readOnly: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'update:content', value: string): void
  (event: 'request-close', done: () => void): void
  (event: 'save'): void
  (event: 'download'): void
}>()

const dialogModel = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value)
})

const contentModel = computed({
  get: () => props.content,
  set: value => emit('update:content', value)
})

const canSave = computed(() => props.mode === 'text' && !props.readOnly)
const canDownload = computed(() => props.mode !== 'text')
const isDocx = computed(() => props.fileName.toLowerCase().endsWith('.docx'))

const pdfCanvas = ref<HTMLCanvasElement | null>(null)
const previewFrame = ref<HTMLDivElement | null>(null)
const wordContainer = ref<HTMLDivElement | null>(null)
const wordStyleContainer = ref<HTMLDivElement | null>(null)
const pdfDoc = shallowRef<any>(null)
const pdfPage = ref(1)
const pdfPageCount = ref(0)
const pdfScale = ref(1)
const lastScrollTop = ref(0)
let renderTask: any = null
let scrollLock = false
let pendingScroll: 'next' | 'prev' | null = null

function resetPdf() {
  if (renderTask?.cancel) {
    renderTask.cancel()
  }
  renderTask = null
  if (pdfDoc.value?.destroy) {
    pdfDoc.value.destroy()
  }
  pdfDoc.value = null
  pdfPage.value = 1
  pdfPageCount.value = 0
  pdfScale.value = 1
  lastScrollTop.value = 0
  scrollLock = false
  pendingScroll = null
}

async function renderPdfPage() {
  if (!pdfDoc.value || !pdfCanvas.value) return
  const page = await pdfDoc.value.getPage(pdfPage.value)
  const viewport = page.getViewport({ scale: pdfScale.value })
  const ratio = window.devicePixelRatio || 1
  const canvas = pdfCanvas.value
  const context = canvas.getContext('2d')
  if (!context) return
  canvas.width = Math.floor(viewport.width * ratio)
  canvas.height = Math.floor(viewport.height * ratio)
  canvas.style.width = `${viewport.width}px`
  canvas.style.height = `${viewport.height}px`
  const transform = ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : null
  if (renderTask?.cancel) {
    renderTask.cancel()
  }
  renderTask = page.render({ canvasContext: context, viewport, transform })
  await renderTask.promise
  if (pendingScroll && previewFrame.value) {
    if (pendingScroll === 'next') {
      previewFrame.value.scrollTop = 0
    } else {
      previewFrame.value.scrollTop = previewFrame.value.scrollHeight
    }
  }
  pendingScroll = null
  scrollLock = false
  if (previewFrame.value) {
    lastScrollTop.value = previewFrame.value.scrollTop
  }
}

async function loadPdf(blob: Blob) {
  resetPdf()
  const data = await blob.arrayBuffer()
  pdfDoc.value = markRaw(await getDocument({ data }).promise)
  pdfPageCount.value = pdfDoc.value.numPages || 1
  pdfPage.value = 1
  pdfScale.value = 1
  await nextTick()
  await renderPdfPage()
}

async function waitForContainerReady(el: HTMLElement, tries = 12) {
  for (let i = 0; i < tries; i += 1) {
    const rect = el.getBoundingClientRect()
    if (rect.width > 0 && rect.height > 0) return
    await new Promise(requestAnimationFrame)
  }
}

async function renderWord(blob: Blob) {
  if (!wordContainer.value) return
  wordContainer.value.innerHTML = ''
  await nextTick()
  await waitForContainerReady(wordContainer.value)
  const data = await blob.arrayBuffer()
  const styleTarget = wordStyleContainer.value || wordContainer.value
  await renderAsync(data, wordContainer.value, styleTarget, {
    inWrapper: true,
    ignoreWidth: false,
    ignoreHeight: false
  })
}

watch(
  () => [props.mode, props.blob, props.modelValue] as const,
  async ([mode, blob, visible]) => {
    if (!visible) {
      resetPdf()
      if (wordContainer.value) wordContainer.value.innerHTML = ''
      if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
      return
    }
    if (mode === 'pdf') {
      if (!blob) {
        resetPdf()
        return
      }
      try {
        await loadPdf(blob)
      } catch (error) {
        console.error('PDF 预览失败:', error)
        resetPdf()
      }
      return
    }
    if (mode === 'word' && blob && isDocx.value) {
      try {
        await renderWord(blob)
      } catch (error) {
        console.error('Word 预览失败:', error)
        if (wordContainer.value) wordContainer.value.innerHTML = ''
        if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
      }
      return
    }
    resetPdf()
    if (wordContainer.value) wordContainer.value.innerHTML = ''
    if (wordStyleContainer.value) wordStyleContainer.value.innerHTML = ''
  }
)

watch([pdfScale, pdfPage], async () => {
  if (props.mode === 'pdf' && pdfDoc.value) {
    await renderPdfPage()
  }
})

onBeforeUnmount(() => {
  resetPdf()
})

function handleBeforeClose(done: () => void) {
  emit('request-close', done)
}

function handleCloseClick() {
  emit('request-close', () => emit('update:modelValue', false))
}

function handleDialogOpened() {
  if (props.mode === 'word' && props.blob && isDocx.value) {
    renderWord(props.blob)
  }
}

function queuePdfPageChange(direction: 'next' | 'prev') {
  if (scrollLock) return
  if (direction === 'next' && pdfPage.value >= pdfPageCount.value) return
  if (direction === 'prev' && pdfPage.value <= 1) return
  scrollLock = true
  pendingScroll = direction
  pdfPage.value += direction === 'next' ? 1 : -1
}

function handlePdfScroll() {
  if (props.mode !== 'pdf') return
  const frame = previewFrame.value
  if (!frame || scrollLock) return
  const { scrollTop, scrollHeight, clientHeight } = frame
  const maxScrollTop = Math.max(0, scrollHeight - clientHeight)
  const goingDown = scrollTop > lastScrollTop.value
  const goingUp = scrollTop < lastScrollTop.value
  lastScrollTop.value = scrollTop
  if (maxScrollTop <= 0) return
  if (goingDown && scrollTop >= maxScrollTop - 32) {
    queuePdfPageChange('next')
  } else if (goingUp && scrollTop <= 32) {
    queuePdfPageChange('prev')
  }
}

function handlePdfWheel(event: WheelEvent) {
  if (props.mode !== 'pdf') return
  const frame = previewFrame.value
  if (!frame || scrollLock) return
  const { scrollTop, scrollHeight, clientHeight } = frame
  const maxScrollTop = Math.max(0, scrollHeight - clientHeight)
  const atBottom = scrollTop >= maxScrollTop - 32
  const atTop = scrollTop <= 32
  const down = event.deltaY > 0
  const up = event.deltaY < 0
  if (down && (maxScrollTop <= 0 || atBottom)) {
    queuePdfPageChange('next')
    event.preventDefault()
  } else if (up && (maxScrollTop <= 0 || atTop)) {
    queuePdfPageChange('prev')
    event.preventDefault()
  }
}
</script>

<template>
  <el-dialog
    v-model="dialogModel"
    :title="title"
    width="760px"
    top="6vh"
    :close-on-click-modal="false"
    :before-close="handleBeforeClose"
    @opened="handleDialogOpened"
    class="file-preview-dialog"
  >
    <div class="preview-body" v-loading="loading">
      <template v-if="mode === 'text'">
        <el-input
          v-model="contentModel"
          type="textarea"
          :rows="18"
          resize="vertical"
          class="preview-textarea"
          :disabled="loading || readOnly"
        />
      </template>
      <template v-else-if="mode === 'pdf'">
        <div v-if="!blob" class="preview-placeholder">正在加载 PDF...</div>
        <template v-else>
          <div class="pdf-toolbar">
            <el-button size="small" :disabled="pdfPage <= 1" @click="pdfPage -= 1">上一页</el-button>
            <span class="pdf-meta">{{ pdfPage }} / {{ pdfPageCount || 1 }}</span>
            <el-button size="small" :disabled="pdfPage >= pdfPageCount" @click="pdfPage += 1">下一页</el-button>
            <div class="pdf-spacer"></div>
            <el-button size="small" @click="pdfScale = Math.max(0.2, pdfScale - 0.1)">缩小</el-button>
            <span class="pdf-meta">{{ Math.round(pdfScale * 100) }}%</span>
            <el-button size="small" @click="pdfScale = Math.min(2.2, pdfScale + 0.1)">放大</el-button>
          </div>
          <div
            ref="previewFrame"
            class="preview-frame"
            @scroll="handlePdfScroll"
            @wheel="handlePdfWheel"
          >
            <canvas ref="pdfCanvas"></canvas>
          </div>
        </template>
      </template>
      <template v-else>
        <div v-if="isDocx && blob" class="preview-docx" ref="wordContainer"></div>
        <div v-if="isDocx && blob" ref="wordStyleContainer" class="docx-style-container"></div>
        <div v-else class="preview-placeholder">
          暂不支持在线预览此类型文件
        </div>
      </template>
    </div>
    <template #footer>
      <el-button @click="handleCloseClick">关闭</el-button>
      <el-button v-if="canDownload" @click="$emit('download')">
        下载
      </el-button>
      <el-button
        v-if="canSave"
        type="primary"
        :loading="saving"
        :disabled="loading || !dirty"
        @click="$emit('save')"
      >
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.preview-body {
  min-height: 380px;
}

.preview-textarea :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  line-height: 1.5;
}

.preview-frame {
  width: 100%;
  height: 60vh;
  min-height: 360px;
  border-radius: 12px;
  overflow: auto;
  border: 1px solid #eef1f4;
  background: #f8fafc;
  display: block;
  padding: 12px;
  overscroll-behavior: contain;
}

.preview-frame canvas {
  display: block;
  margin: 0 auto;
}

.pdf-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.pdf-meta {
  font-size: 12px;
  color: #606266;
}

.pdf-spacer {
  flex: 1;
  min-width: 12px;
}

.preview-docx {
  min-height: 320px;
  padding: 12px;
  background: #fff;
  border: 1px solid #eef1f4;
  border-radius: 12px;
  overflow: auto;
}

.docx-style-container {
  display: none;
}

.preview-placeholder {
  height: 260px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #909399;
  font-size: 14px;
  background: #f8fafc;
  border: 1px dashed #dcdfe6;
  border-radius: 12px;
}
</style>
