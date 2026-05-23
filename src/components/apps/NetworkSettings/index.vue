<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { NButton, NCard, NForm, NFormItem, NInput, NInputNumber, NSpace, NSwitch, useMessage } from 'naive-ui'
import { getFaviconNetwork, setFaviconNetwork, testFaviconNetwork } from '@/api/panel/networkSetting'
import { t } from '@/locales'

const defaultTestUrl = 'https://github.com/lobehub/lobehub/blob/canary/README.zh-CN.md'
const ms = useMessage()
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const testUrl = ref(defaultTestUrl)
const testResult = ref<NetworkSetting.FaviconNetworkTestResponse | null>(null)

const form = reactive<NetworkSetting.FaviconNetworkSetting>({
  proxyUrl: '',
  proxyFromEnv: false,
  noProxy: '',
  timeoutSeconds: 10,
})

const visibleIconUrls = computed(() => testResult.value?.iconUrls.slice(0, 3) ?? [])

function updateForm(setting: NetworkSetting.FaviconNetworkSetting) {
  form.proxyUrl = setting.proxyUrl
  form.proxyFromEnv = setting.proxyFromEnv
  form.noProxy = setting.noProxy
  form.timeoutSeconds = setting.timeoutSeconds
}

function handleTimeoutUpdate(value: number | null) {
  form.timeoutSeconds = value ?? 0
}

async function loadSetting() {
  loading.value = true
  try {
    const { code, data } = await getFaviconNetwork<NetworkSetting.FaviconNetworkSetting>()
    if (code === 0)
      updateForm(data)
    else
      ms.error(t('apps.networkSettings.loadFailed'))
  }
  catch (error) {
    ms.error(t('apps.networkSettings.loadFailed'))
  }
  finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const { code, data } = await setFaviconNetwork<NetworkSetting.FaviconNetworkSetting>({ ...form })
    if (code === 0) {
      updateForm(data)
      ms.success(t('common.saveSuccess'))
    }
  }
  finally {
    saving.value = false
  }
}

async function handleTest() {
  testing.value = true
  testResult.value = null
  try {
    const { code, data } = await testFaviconNetwork<NetworkSetting.FaviconNetworkTestResponse>({
      url: testUrl.value,
      setting: { ...form },
    })
    if (code === 0) {
      testResult.value = data
      ms.success(t('apps.networkSettings.testSuccess'))
    }
    else {
      ms.error(t('apps.networkSettings.testFailed'))
    }
  }
  catch (error) {
    ms.error(t('apps.networkSettings.testFailed'))
  }
  finally {
    testing.value = false
  }
}

onMounted(() => {
  loadSetting()
})
</script>

<template>
  <div class="bg-slate-200 dark:bg-zinc-900 p-2 h-full overflow-auto">
    <NCard
      :title="$t('apps.networkSettings.faviconFetch')"
      size="small"
      style="border-radius: 5px;"
      :bordered="false"
    >
      <NForm :model="form" label-placement="top" :show-feedback="false" :disabled="loading">
        <NFormItem :label="$t('apps.networkSettings.proxyUrl')">
          <NInput v-model:value="form.proxyUrl" clearable />
        </NFormItem>
        <NFormItem :label="$t('apps.networkSettings.proxyFromEnv')">
          <NSwitch v-model:value="form.proxyFromEnv" />
        </NFormItem>
        <NFormItem :label="$t('apps.networkSettings.noProxy')">
          <NInput
            v-model:value="form.noProxy"
            type="textarea"
            clearable
            :autosize="{ minRows: 3, maxRows: 6 }"
          />
        </NFormItem>
        <NFormItem :label="$t('apps.networkSettings.timeoutSeconds')">
          <NInputNumber
            :value="form.timeoutSeconds"
            :min="1"
            :max="120"
            class="w-full"
            @update:value="handleTimeoutUpdate"
          />
        </NFormItem>
        <NSpace justify="end">
          <NButton type="primary" :loading="saving" @click="handleSave">
            {{ $t('common.save') }}
          </NButton>
        </NSpace>
      </NForm>
    </NCard>

    <NCard
      class="mt-2"
      size="small"
      style="border-radius: 5px;"
      :bordered="false"
    >
      <NSpace vertical>
        <NInput v-model:value="testUrl" clearable />
        <NSpace justify="end">
          <NButton :loading="testing" @click="handleTest">
            {{ $t('apps.networkSettings.test') }}
          </NButton>
        </NSpace>
        <div v-if="testResult" class="text-xs leading-6">
          <div>
            <span class="text-slate-500">Proxy:</span>
            <span class="ml-1 break-all">{{ testResult.proxy || '-' }}</span>
          </div>
          <div v-for="iconUrl in visibleIconUrls" :key="iconUrl" class="break-all">
            {{ iconUrl }}
          </div>
        </div>
      </NSpace>
    </NCard>
  </div>
</template>
