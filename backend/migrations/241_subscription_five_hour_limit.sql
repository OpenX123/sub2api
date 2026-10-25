-- 订阅分组 5h 窗口限额（拼车套餐）。
-- 限额配在分组上，用量按每个订阅独立累计；窗口语义与 Claude 官方 5h 限额一致：
-- 首次计费开启窗口，满 5h 过期，过期后的下一次计费开启新窗口。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS rate_limit_5h DECIMAL(20, 8);

COMMENT ON COLUMN groups.rate_limit_5h IS '订阅分组每个订阅的 5h 窗口 USD 限额：NULL 不限，0 禁止使用，>0 为上限';

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS usage_5h DECIMAL(20, 10) NOT NULL DEFAULT 0;

COMMENT ON COLUMN user_subscriptions.usage_5h IS '当前 5h 窗口已用 USD；窗口过期后下一次计费从本次费用重新累计';

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS window_5h_start TIMESTAMPTZ;

COMMENT ON COLUMN user_subscriptions.window_5h_start IS '当前 5h 窗口起点：窗口为空或已过期时，下一次计费写入当时的时间';
