package main

import (
	"fmt"
	"math/rand"
)

var alerts = []string{
	"HighPodRestartRate", "DiskPressureWarning", "CertExpiryImminent", "QueueDepthGrowing",
	"LatencyBudgetBurn", "OOMKilledSpike", "NodeNotReady", "ReplicaLagHigh",
	"IngressErrorRate", "BackupJobFailed", "CronDrift", "TLSHandshakeErrors",
	"CacheEvictionStorm", "ThrottledRequests", "DNSResolutionSlow", "PVCAlmostFull",
	"DeploymentStuck", "ImagePullBackoff", "HPAAtMax", "ConnPoolExhausted",
	"LogVolumeSurge", "ShardImbalance", "ColdStartSpike", "RetryStormDetected",
}

var hosts = []string{
	"prod-api-east", "prod-api-west", "prod-worker-a", "prod-worker-b", "prod-ingest",
	"prod-search", "prod-billing", "staging-api", "prod-scheduler", "prod-gateway",
	"prod-reports", "prod-webhooks", "prod-etl", "prod-notify", "prod-auth",
	"prod-files", "prod-metrics", "prod-relay",
}

var agentAsks = []string{
	"correlate with the last deploy and say if we should roll back",
	"check whether the error budget for the tier is already burned",
	"summarize probable root cause and next diagnostic step",
	"decide if this needs a page or can wait for business hours",
	"compare with the same window last week and flag anomalies",
	"list the top three suspects and the command to confirm each",
	"draft a status-page note if customer impact is likely",
	"check recent config changes that could explain this",
}

var toolNames = []string{"kubectl_get", "promql_query", "loki_search", "deploy_history", "runbook_lookup"}

func agentFile(r *rand.Rand) map[string]any {
	const n = 430
	ts := timeline(r, n)
	spans := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		alert := alerts[r.Intn(len(alerts))]
		host := hosts[r.Intn(len(hosts))]
		ask := agentAsks[r.Intn(len(agentAsks))]
		isErr := r.Float64() < 0.08
		wantsTool := r.Float64() < 0.70

		attrs := []map[string]any{
			kvStr("gen_ai.system", "anthropic"),
			kvStr("llm.request.model", "claude-sonnet-4-20250514"), // OpenLLMetry spelling
			kvIntStr("llm.usage.prompt_tokens", 5500+int64(r.Intn(3500))),
			kvIntStr("llm.usage.completion_tokens", 600+int64(r.Intn(700))),
			kvStr("gen_ai.agent.name", "ops-agent"),
			kvStr("gen_ai.prompt.0.role", "system"),
			kvStr("gen_ai.prompt.0.content", "You are ops-agent."),
			kvStr("gen_ai.prompt.1.role", "user"),
			kvStr("gen_ai.prompt.1.content",
				fmt.Sprintf("Alert %s firing on %s since %02d:%02d UTC; %s.", alert, host, r.Intn(24), r.Intn(60), ask)),
		}
		if r.Float64() < 0.5 { // some iterations carry a prior tool result
			attrs = append(attrs,
				kvStr("gen_ai.prompt.2.role", "tool"),
				kvStr("gen_ai.prompt.2.content",
					fmt.Sprintf(`{"tool":%q,"rows":%d,"truncated":%v}`, toolNames[r.Intn(len(toolNames))], r.Intn(400), r.Float64() < 0.3)),
			)
		}
		if wantsTool && !isErr {
			tool := toolNames[r.Intn(len(toolNames))]
			attrs = append(attrs,
				kvStr("gen_ai.completion.0.finish_reason", "tool_calls"),
				kvStr("gen_ai.completion.0.tool_calls.0.name", tool),
				kvStr("gen_ai.completion.0.tool_calls.0.arguments",
					fmt.Sprintf(`{"target":%q,"window":"15m"}`, host)),
			)
		} else if !isErr {
			attrs = append(attrs,
				kvStr("gen_ai.completion.0.finish_reason", "stop"),
				kvStr("gen_ai.completion.0.content",
					fmt.Sprintf("Most likely cause on %s is %s pressure; verify with %s before acting.",
						host, alert, toolNames[r.Intn(len(toolNames))])),
			)
		}
		dur := 2000 + r.Float64()*6000
		if r.Float64() < 0.05 {
			dur = 25000 + r.Float64()*15000 // stuck-loop latency outliers
		}
		spans = append(spans, mkSpan(r, "chat claude-sonnet-4", ts[i], dur, attrs, isErr))
	}
	return otlpDoc("ops-agent", spans)
}
