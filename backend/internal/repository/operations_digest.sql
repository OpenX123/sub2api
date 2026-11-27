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
), blocked_categories AS (
 SELECT highest_category,sum(blocks) blocks,count(user_id) users,max(blocks) peak
 FROM risk WHERE blocks>0 GROUP BY highest_category
 ORDER BY blocks DESC,highest_category LIMIT 3
), hourly AS (
 SELECT date_trunc('hour',created_at) bucket,user_id,count(*) requests,sum(actual_cost) cost,
 sum(input_tokens::bigint+output_tokens+cache_creation_tokens+cache_read_tokens) tokens
 FROM usage_logs WHERE created_at >= $1::timestamptz-interval '24 hours' AND created_at < $1
 GROUP BY 1,2
), analysis_windows AS (
 SELECT n, jsonb_build_object(
 'requests',coalesce(sum(h.requests),0),'tokens',coalesce(sum(h.tokens),0),
 'cost_usd',round(coalesce(sum(h.cost),0),4),
 'errors',CASE WHEN n=0 THEN (SELECT count(*) FROM ops_error_logs WHERE created_at >= $1::timestamptz-interval '3 hours' AND created_at < $1)
 ELSE (SELECT count(*) FROM ops_error_logs WHERE created_at >= $1::timestamptz-interval '6 hours' AND created_at < $1::timestamptz-interval '3 hours') END
 ) metrics FROM generate_series(0,1) n LEFT JOIN hourly h
 ON h.bucket >= $1::timestamptz - (n+1)*interval '3 hours'
 AND h.bucket < $1::timestamptz - n*interval '3 hours' GROUP BY n
), volumes AS (
 SELECT user_id,sum(tokens) tokens,sum(cost) cost,sum(requests) requests,
 sum(CASE WHEN bucket >= $1::timestamptz-interval '3 hours' THEN tokens
          WHEN bucket >= $1::timestamptz-interval '6 hours' THEN -tokens ELSE 0 END) growth
 FROM hourly GROUP BY user_id
), leaders AS (
 SELECT *,row_number() over(order by tokens DESC,user_id) pos FROM volumes WHERE tokens>0
), selected AS (
 SELECT * FROM leaders WHERE pos<=3
 UNION ALL
 (SELECT * FROM leaders WHERE pos>3 AND growth>0 ORDER BY growth DESC,user_id LIMIT 2)
), names AS (
 SELECT s.*,coalesce(nullif(btrim(u.username),''),nullif(btrim(u.email),''),'未命名用户') base_name
 FROM selected s LEFT JOIN users u ON u.id=s.user_id
), trend_users AS (
 SELECT n.*,CASE WHEN count(*) OVER (PARTITION BY base_name)>1
 THEN format('%s（%s）',base_name,row_number() OVER (PARTITION BY base_name ORDER BY user_id))
 ELSE base_name END display_name FROM names n
), points AS (
 SELECT h.bucket,s.user_id,s.display_name,coalesce(u.tokens,0) tokens
 FROM generate_series($1::timestamptz-interval '24 hours',$1::timestamptz-interval '1 hour',interval '1 hour') h(bucket)
 CROSS JOIN trend_users s LEFT JOIN hourly u ON u.user_id=s.user_id AND u.bucket=h.bucket
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
), active_users AS (
 SELECT user_id FROM hourly WHERE bucket >= $1::timestamptz-interval '3 hours'
 UNION SELECT user_id FROM errors WHERE user_id IS NOT NULL
), error_groups AS (
 SELECT error_type,status_code,error_owner,sum(hits) hits,count(user_id) users,
 string_agg(format('%s（%s 次）',coalesce(u.email,'ID '||user_id::text,'身份未知'),hits),'、' order by pos)
 FILTER (WHERE pos<=3) names FROM error_rank e LEFT JOIN users u ON u.id=e.user_id
 GROUP BY error_type,status_code,error_owner
), lines AS (
 SELECT 2 sec,pos,format('%s · %s（ID %s） · 命中 %s / 拦截 %s · %s · 最高 %s',
 CASE WHEN banned THEN '自动封禁' WHEN blocks>=2 THEN '多次拦截' WHEN blocks=1 THEN '单次拦截' ELSE '观察命中' END,
 email,user_id,hits,blocks,category,round(score::numeric,2)) line FROM risk_rank WHERE pos<=10
 UNION ALL SELECT 3,pos,format('%s. %s（ID %s） · $%s · %s 次请求',pos,coalesce(u.email,'未知用户'),r.user_id,round(cost::numeric,4),requests)
 FROM spend_rank r LEFT JOIN users u ON u.id=r.user_id WHERE pos<=10
 UNION ALL SELECT 4,0,format('主要错误 · 共 %s 条记录（前 5 类）',coalesce(sum(hits),0)) FROM error_groups
 UNION ALL SELECT 4,row_number() over(order by hits DESC,error_type,status_code,error_owner),
 format('%s / HTTP %s · %s 次 · %s 位已识别用户 · %s：%s',error_type,status_code,hits,users,
 CASE WHEN error_owner='client' THEN '触发用户' ELSE '受影响用户' END,names) FROM error_groups
)
SELECT jsonb_build_object(
 'AnalysisCurrent',(SELECT metrics FROM analysis_windows WHERE n=0),
 'AnalysisPrevious',(SELECT metrics FROM analysis_windows WHERE n=1),
 'RPM',(SELECT count(*)::text FROM claude WHERE created_at >= $2::timestamptz-interval '1 minute'),
 'Latency',(SELECT coalesce(round((percentile_cont(.95) within group(order by first_token_ms) filter(where stream))::numeric/1000,1)::text||' 秒','无样本') FROM claude),
 'Duration',(SELECT coalesce(round((percentile_cont(.95) within group(order by duration_ms))::numeric/1000,1)::text||' 秒','无样本') FROM claude),
 'Highlight',coalesce((SELECT CASE WHEN banned THEN format('🟠 用户 #%s 已自动封禁',user_id) WHEN blocks>=2 THEN format('🟠 用户 #%s 被拦截 %s 次',user_id,blocks) ELSE '' END FROM risk_rank WHERE pos=1),''),
 'Risk',(SELECT format('🛡️ Jev　%s 人命中 · %s 人重复拦截',count(*),count(*) filter(where blocks>=2)) FROM risk_users),
 'Spend',(SELECT format('今日消费　$%s',round(coalesce(sum(cost),0),2)) FROM spend)||coalesce((SELECT format(' · #%s 最高 $%s',user_id,round(cost::numeric,2)) FROM spend_rank WHERE pos=1),''),
 'Errors',coalesce((SELECT format('主要错误　%s · %s 次 · %s 人',case when error_owner='client' then '请求' when error_owner='provider' then '上游' else '服务' end || ' ' || status_code,hits,users) FROM error_groups ORDER BY hits DESC,error_type,status_code,error_owner LIMIT 1),'主要错误　无记录'),
 'RiskRows',(SELECT jsonb_agg(line ORDER BY pos) FROM lines WHERE sec=2 AND pos>0),
 'SpendRows',(SELECT jsonb_agg(line ORDER BY pos) FROM lines WHERE sec=3 AND pos>0),
 'ErrorRows',(SELECT jsonb_agg(line ORDER BY pos) FROM lines WHERE sec=4 AND pos BETWEEN 1 AND 5)
 ,'Trend',(SELECT jsonb_agg(jsonb_build_object('hour',to_char(bucket AT TIME ZONE 'Asia/Shanghai','HH24:MI'),'user',display_name,'millions',round(tokens::numeric/1000000,4)) ORDER BY bucket,user_id) FROM points)
 ,'TrendRows',(SELECT jsonb_agg(format('%s · %s 次请求 · %s M Token · $%s · 近 3h 增减 %s M',display_name,requests,round(tokens/1000000,2),round(cost,2),round(growth/1000000,2)) ORDER BY pos) FROM trend_users)
 ,'TokenShare',(SELECT round(100*sum(tokens) FILTER (WHERE pos<=3)/nullif(sum(tokens),0),1)::text FROM leaders)
 ,'SpendShare',(SELECT round(100*sum(cost) FILTER (WHERE pos<=3)/nullif(sum(cost),0),1)::text FROM spend_rank)
 ,'ErrorImpact',(SELECT format('%s / %s 位活跃用户（%s）',count(DISTINCT user_id),(SELECT count(*) FROM active_users),
 coalesce(round(100.0*count(DISTINCT user_id)/nullif((SELECT count(*) FROM active_users),0),1)::text||'%','无样本')) FROM errors)
 ,'Examples',(SELECT jsonb_agg(format('%s · 拦截 %s 次 · %s 位已识别用户 · 单一身份最多 %s 次%s',
 b.highest_category,b.blocks,b.users,b.peak,format(E'\n最近一例　%s · %s · 评分 %s',
 to_char(e.created_at AT TIME ZONE 'Asia/Shanghai','HH24:MI'),coalesce(nullif(e.model,''),'模型未知'),round(e.highest_score,2)))
 ORDER BY b.blocks DESC,b.highest_category) FROM blocked_categories b
 JOIN LATERAL (SELECT created_at,model,highest_score FROM content_moderation_logs
 WHERE created_at >= $1::timestamptz-interval '3 hours' AND created_at < $1
 AND flagged AND action='block' AND engine_meta->>'engine'='typesafe' AND highest_category=b.highest_category
 ORDER BY created_at DESC,id DESC LIMIT 1) e ON true)
)
