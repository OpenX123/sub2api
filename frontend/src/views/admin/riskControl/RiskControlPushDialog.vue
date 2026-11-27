<template>
  <BaseDialog :show="show" :title="t('admin.riskControl.push.title')" width="normal" @close="emit('close')">
    <div class="space-y-5">
      <div class="flex items-center justify-between gap-4">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.push.enabled') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.push.enabledHint') }}</p>
        </div>
        <Toggle v-model="form.enabled" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.riskControl.push.sendKey') }}</label>
        <input
          v-model.trim="form.send_key"
          type="password"
          autocomplete="off"
          class="input"
          data-test="push-send-key"
          :placeholder="configured ? maskedKey : 'https://open.feishu.cn/open-apis/bot/v2/hook/...'"
        />
        <p class="input-hint">
          {{ configured ? t('admin.riskControl.push.sendKeyConfigured', { masked: maskedKey }) : t('admin.riskControl.push.sendKeyHint') }}
        </p>
      </div>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.hitDailyCap') }}</label>
          <input v-model.number="form.hit_daily_cap" type="number" min="1" max="1000" class="input" data-test="push-hit-cap" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.hitWindowMinutes') }}</label>
          <input v-model.number="form.hit_window_minutes" type="number" min="5" max="240" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.dailyReportHour') }}</label>
          <input v-model.number="form.daily_report_hour" type="number" min="0" max="23" class="input" />
        </div>
      </div>
      <div class="flex items-center justify-between">
        <p class="text-sm text-gray-900 dark:text-white">{{ t('admin.riskControl.push.dailyReportEnabled') }}</p>
        <Toggle v-model="form.daily_report_enabled" />
      </div>
      <div class="flex items-center justify-between">
        <p class="text-sm text-gray-900 dark:text-white">{{ t('admin.riskControl.push.opsCriticalEnabled') }}</p>
        <Toggle v-model="form.ops_critical_enabled" />
      </div>
      <div class="flex items-center justify-between">
        <p class="text-sm text-gray-900 dark:text-white">{{ t('admin.riskControl.push.accountAlertEnabled') }}</p>
        <Toggle v-model="form.account_alert_enabled" />
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="testing || !configured" data-test="push-test" @click="sendTest">
          {{ t('admin.riskControl.push.sendTest') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving" data-test="push-save" @click="save">
          {{ t('admin.riskControl.push.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminAPI } from '@/api/admin'
import type { PushNotificationConfig, UpdatePushNotificationConfig } from '@/api/admin/riskControl'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()

const configured = ref(false)
const maskedKey = ref('')
const saving = ref(false)
const testing = ref(false)
const form = reactive({
  enabled: false,
  send_key: '',
  hit_daily_cap: 300,
  hit_window_minutes: 30,
  daily_report_enabled: true,
  daily_report_hour: 9,
  ops_critical_enabled: true,
  account_alert_enabled: true,
})

function apply(cfg: PushNotificationConfig) {
  configured.value = cfg.send_key_configured
  maskedKey.value = cfg.send_key_masked
  Object.assign(form, {
    enabled: cfg.enabled,
    send_key: '',
    hit_daily_cap: cfg.hit_daily_cap,
    hit_window_minutes: cfg.hit_window_minutes,
    daily_report_enabled: cfg.daily_report_enabled,
    daily_report_hour: cfg.daily_report_hour,
    ops_critical_enabled: cfg.ops_critical_enabled,
    account_alert_enabled: cfg.account_alert_enabled,
  })
}

async function load() {
  try {
    apply(await adminAPI.riskControl.getPushConfig())
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.loadFailed')))
  }
}

async function save() {
  saving.value = true
  try {
    const payload: UpdatePushNotificationConfig = {
      enabled: form.enabled,
      hit_daily_cap: Number(form.hit_daily_cap),
      hit_window_minutes: Number(form.hit_window_minutes),
      daily_report_enabled: form.daily_report_enabled,
      daily_report_hour: Number(form.daily_report_hour),
      ops_critical_enabled: form.ops_critical_enabled,
      account_alert_enabled: form.account_alert_enabled,
    }
    if (form.send_key !== '') {
      payload.send_key = form.send_key
    }
    apply(await adminAPI.riskControl.updatePushConfig(payload))
    appStore.showSuccess(t('admin.riskControl.push.saved'))
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function sendTest() {
  testing.value = true
  try {
    await adminAPI.riskControl.sendPushTest()
    appStore.showSuccess(t('admin.riskControl.push.testSent'))
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.testFailed')))
  } finally {
    testing.value = false
  }
}

watch(
  () => props.show,
  (visible) => {
    if (visible) void load()
  },
  { immediate: true },
)
</script>
