import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionProgressMini from '../SubscriptionProgressMini.vue'
const store = vi.hoisted(() => ({ activeSubscriptions: [] as unknown[], hasActiveSubscriptions: true, fetchActiveSubscriptions: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/stores', () => ({ useSubscriptionStore: () => store }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { subscription: 'subscription' }, isFeatureFlagEnabled: () => true }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 8, 22, 12)) })
afterEach(() => vi.useRealTimers())
describe('subscription expiry calendar labels', () => {
  it.each([
    [new Date(2026, 8, 22, 18), 'expiresToday'],
    [new Date(2026, 8, 23, 18), 'expiresTomorrow'],
    [new Date(2026, 8, 22, 12), 'expired'],
    [new Date(2026, 8, 25, 12), 'daysRemaining'],
  ])('labels %s as %s', async (expires, label) => {
    store.activeSubscriptions = [{ id: 1, group_id: 1, expires_at: expires.toISOString(), group: { name: 'Plan' } }]
    const w = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await w.get('button').trigger('click')
    expect(w.text()).toContain('subscriptionProgress.' + label)
  })
})

describe('subscription 5h window progress', () => {
  it('shows 5h usage against the group limit', async () => {
    store.activeSubscriptions = [{ id: 1, group_id: 1, expires_at: new Date(2026, 9, 22).toISOString(), usage_5h_usd: 3, group: { name: 'Carpool', rate_limit_5h: 12 } }]
    const w = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await w.get('button').trigger('click')
    expect(w.text()).toContain('subscriptionProgress.fiveHour')
    expect(w.text()).toContain('$3.00')
    expect(w.text()).toContain('12.00')
    expect(w.text()).not.toContain('subscriptionProgress.unlimited')
  })

  it('treats a zero 5h limit as blocked rather than unlimited', async () => {
    store.activeSubscriptions = [{ id: 1, group_id: 1, expires_at: new Date(2026, 9, 22).toISOString(), group: { name: 'Carpool', rate_limit_5h: 0 } }]
    const w = mount(SubscriptionProgressMini, { global: { stubs: { Icon: true, RouterLink: true } } })
    await w.get('button').trigger('click')
    expect(w.text()).toContain('subscriptionProgress.fiveHour')
    expect(w.text()).not.toContain('subscriptionProgress.unlimited')
    expect(w.find('.bg-red-500').exists()).toBe(true)
  })
})
