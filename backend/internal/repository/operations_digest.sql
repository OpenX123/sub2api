WITH claude AS (
 SELECT u.created_at,u.stream,u.duration_ms,u.first_token_ms FROM usage_logs u
 JOIN accounts a ON a.id=u.account_id WHERE a.platform='anthropic'
 AND u.created_at >= $2::timestamptz - interval '15 minutes' AND u.created_at < $2
), risk AS (
 SELECT user_id,max(user_email) email,highest_category,count(*) hits,
 count(*) FILTER (WHERE action='block') blocks,bool_or(auto_banned) banned,max(highest_score) score
 FROM content_moderation_logs WHERE created_at >= $1::timestamptz - interval '3 hours' AND created_at < $1
 AND flagged AND engine_meta->>'engine'='typesafe' AND action IN ('block','allow')
 GROUP BY user_id,highest_category
), risk_users AS (
 SELECT user_id,max(email) email,sum(hits) hits,sum(blocks) blocks,bool_or(banned) banned,max(score) score,
 (array_agg(highest_category ORDER BY hits DESC,highest_category))[1] category
 FROM risk GROUP BY user_id
), risk_rank AS (
 SELECT *,count(*) over() n,row_number() over(order by banned DESC,blocks DESC,hits DESC,user_id) pos FROM risk_users
), spend AS (
 SELECT user_id,sum(actual_cost) cost,count(*) requests FROM usage_logs
 WHERE created_at >= $3::timestamptz AND created_at < $2 GROUP BY user_id
), spend_rank AS (
 SELECT s.*,row_number() over(order by cost DESC,user_id) pos FROM spend s
), errors AS (
 SELECT error_type,status_code,error_owner,user_id,count(*) hits FROM ops_error_logs
 WHERE created_at >= $1::timestamptz - interval '3 hours' AND created_at < $1
 GROUP BY error_type,status_code,error_owner,user_id
), error_rank AS (
 SELECT e.*,row_number() over(partition by error_type,status_code,error_owner order by hits DESC,user_id) pos
 FROM errors e
), error_groups AS (
 SELECT error_type,status_code,error_owner,sum(hits) hits,count(user_id) users,
 string_agg(format('%s（%s 次）',coalesce(u.email,'ID '||user_id::text,'身份未知'),hits),'、' order by pos)
 FILTER (WHERE pos<=3) names FROM error_rank e LEFT JOIN users u ON u.id=e.user_id
 GROUP BY error_type,status_code,error_owner
), lines AS (
 SELECT 1 sec,0::bigint pos,format('最近 1 分钟完成请求 %s RPM · 近 15 分钟流式首字 P95 %s · 总耗时 P95 %s',
 count(*) filter(where created_at >= $2::timestamptz - interval '1 minute'),
 coalesce(round((percentile_cont(.95) within group(order by first_token_ms) filter(where stream))::numeric)::text||' ms','无样本'),
 coalesce(round((percentile_cont(.95) within group(order by duration_ms))::numeric)::text||' ms','无样本')) line FROM claude
 UNION ALL SELECT 2,0,format('Jev 风控 · %s 位用户（按严重程度，最多列 10 位）',count(*)) FROM risk_users
 UNION ALL SELECT 2,pos,format('%s · %s（ID %s） · 命中 %s / 拦截 %s · %s · 最高 %s',
 CASE WHEN banned THEN '自动封禁' WHEN blocks>=2 THEN '多次拦截' WHEN blocks=1 THEN '单次拦截' ELSE '观察命中' END,
 email,user_id,hits,blocks,category,round(score::numeric,2)) FROM risk_rank WHERE pos<=10
 UNION ALL SELECT 3,0,format('今日消费 · 合计 $%s（前 10 名）',round(coalesce(sum(cost),0),4)) FROM spend
 UNION ALL SELECT 3,pos,format('%s. %s（ID %s） · $%s · %s 次请求',pos,coalesce(u.email,'未知用户'),r.user_id,round(cost::numeric,4),requests)
 FROM spend_rank r LEFT JOIN users u ON u.id=r.user_id WHERE pos<=10
 UNION ALL SELECT 4,0,format('主要错误 · 共 %s 条记录（前 5 类）',coalesce(sum(hits),0)) FROM error_groups
 UNION ALL SELECT 4,row_number() over(order by hits DESC,error_type,status_code,error_owner),
 format('%s / HTTP %s · %s 次 · %s 位已识别用户 · %s：%s',error_type,status_code,hits,users,
 CASE WHEN error_owner='client' THEN '触发用户' ELSE '受影响用户' END,names) FROM error_groups
)
SELECT line FROM lines WHERE sec<>4 OR pos<=5 ORDER BY sec,pos