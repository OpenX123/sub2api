import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import RiskControlPushDialog from '../RiskControlPushDialog.vue'

const { getPushConfig, updatePushConfig, sendPushTest, showError, showSuccess } = vi.hoisted(() => ({
  getPushConfig: vi.fn(),
  updatePushConfig: vi.fn(),
  sendPushTest: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({ adminAPI: { riskControl: { getPushConfig, updatePushConfig, sendPushTest } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  setup(props, { slots }) {
    return () => (props.show ? h('div', [slots.default?.(), slots.footer?.()]) : null)
  },
})

const config = () => ({
  enabled: true,
  send_key_configured: true,
  send_key_masked: '********F123',
  hit_daily_cap: 300,
  hit_window_minutes: 30,
  daily_report_enabled: true,
  daily_report_hour: 9,
  ops_critical_enabled: true,
})

function mountDialog() {
  return mount(RiskControlPushDialog, { props: { show: true }, global: { stubs: { BaseDialog: BaseDialogStub, Toggle: true } } })
}

describe('RiskControlPushDialog', () => {
  beforeEach(() => {
    getPushConfig.mockReset().mockResolvedValue(config())
    updatePushConfig.mockReset().mockImplementation(async (p) => ({ ...config(), ...p }))
    sendPushTest.mockReset().mockResolvedValue({ message: 'ok' })
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('shows the masked key and never sends it back when unchanged', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.get('[data-test="push-send-key"]').attributes('placeholder')).toBe('********F123')
    await wrapper.get('[data-test="push-hit-cap"]').setValue('200')
    await wrapper.get('[data-test="push-save"]').trigger('click')
    await flushPromises()
    const payload = updatePushConfig.mock.calls[0][0]
    expect(payload.hit_daily_cap).toBe(200)
    expect(payload).not.toHaveProperty('send_key')
    expect(showSuccess).toHaveBeenCalled()
  })

  it('sends a new key and triggers a test push', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="push-send-key"]').setValue('SCT999newKEY')
    await wrapper.get('[data-test="push-save"]').trigger('click')
    await flushPromises()
    expect(updatePushConfig.mock.calls[0][0].send_key).toBe('SCT999newKEY')
    await wrapper.get('[data-test="push-test"]').trigger('click')
    await flushPromises()
    expect(sendPushTest).toHaveBeenCalledTimes(1)
  })
})
