-- 审计观察名单：把现有风控白名单复制进内容审计配置的 observe_user_ids，
-- 让升级后"只记录不拦截"的行为保持不变；风控白名单此后只管 OpenAI cyber_policy。
-- 只在配置里还没有 observe_user_ids 时写入，可重复执行；配置不是合法 JSON 时跳过。
DO $$
BEGIN
    UPDATE settings AS cfg
    SET value = jsonb_set(
            cfg.value::jsonb,
            '{observe_user_ids}',
            COALESCE((
                SELECT jsonb_agg(DISTINCT id::bigint ORDER BY id::bigint)
                FROM regexp_split_to_table(trim(allow.value), '[,[:space:]]+') AS id
                WHERE id ~ '^[0-9]+$' AND id::bigint > 0
            ), '[]'::jsonb)
        )::text,
        updated_at = NOW()
    FROM settings AS allow
    WHERE cfg.key = 'content_moderation_config'
      AND allow.key = 'cyber_policy_user_allowlist'
      AND trim(cfg.value) <> ''
      AND NOT (cfg.value::jsonb ? 'observe_user_ids');
EXCEPTION
    WHEN invalid_text_representation THEN
        RAISE NOTICE 'content_moderation_config is not valid JSON; observe_user_ids not migrated';
END $$;
