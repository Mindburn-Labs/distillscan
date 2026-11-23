package ingest

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// GenAI attribute normalization
//
// The alias table maps three attribute generations onto one trace.Call.
// Emitters with equivalent attributes can share the existing normalization.
//
//	legacy      gen_ai.system, gen_ai.usage.prompt_tokens / completion_tokens
//	current     gen_ai.provider.name, gen_ai.usage.input_tokens / output_tokens,
//	            gen_ai.request.model / gen_ai.response.model,
//	            gen_ai.input.messages / gen_ai.output.messages (JSON strings)
//	OpenLLMetry indexed prompts gen_ai.prompt.{n}.content / gen_ai.completion.{n}.*
//	            plus llm.* spellings (handled below: indexed keys need parsing)

type field int

const (
	fProvider field = iota
	fReqModel
	fRespModel
	fInputTokens
	fOutputTokens
	fOperation
	fTask
	fErrorType
	fInputMessages  // JSON-encoded message list (current gen)
	fOutputMessages // JSON-encoded message list (current gen)
	fPromptRaw      // plain string prompt (some emitters)
	fCompletionRaw  // plain string completion (some emitters)
	fFinishReasons
)

var aliasTable = map[string]field{
	// provider
	"gen_ai.provider.name": fProvider, // current
	"gen_ai.system":        fProvider, // legacy
	"llm.vendor":           fProvider, // early OpenLLMetry

	// model
	"gen_ai.request.model":  fReqModel,
	"llm.request.model":     fReqModel,
	"gen_ai.response.model": fRespModel,
	"llm.response.model":    fRespModel,

	// usage
	"gen_ai.usage.input_tokens":      fInputTokens,  // current
	"gen_ai.usage.prompt_tokens":     fInputTokens,  // legacy
	"llm.usage.prompt_tokens":        fInputTokens,  // OpenLLMetry
	"gen_ai.usage.output_tokens":     fOutputTokens, // current
	"gen_ai.usage.completion_tokens": fOutputTokens, // legacy
	"llm.usage.completion_tokens":    fOutputTokens, // OpenLLMetry

	// operation / task-endpoint hints (first non-empty wins, see taskAliases)
	"gen_ai.operation.name": fOperation,
	"llm.request.type":      fOperation,
	"app.task":              fTask,
	"task.name":             fTask,
	"workflow.name":         fTask,
	"gen_ai.agent.name":     fTask,
	"http.route":            fTask,

	// error / content
	"error.type":                     fErrorType,
	"gen_ai.input.messages":          fInputMessages,
	"gen_ai.output.messages":         fOutputMessages,
	"gen_ai.prompt":                  fPromptRaw,
	"gen_ai.completion":              fCompletionRaw,
	"gen_ai.response.finish_reasons": fFinishReasons,
}

// taskAliases orders task hints by trustworthiness; the first present wins.
var taskAliases = []string{"app.task", "task.name", "workflow.name", "gen_ai.agent.name", "http.route"}

// Indexed OpenLLMetry-style prompt/completion attributes.
var (
	rePromptIdx     = regexp.MustCompile(`^gen_ai\.prompt\.(\d+)\.(content|role)$`)
	reCompletionIdx = regexp.MustCompile(`^gen_ai\.completion\.(\d+)\.(content|role|finish_reason)$`)
	reToolCallIdx   = regexp.MustCompile(`^gen_ai\.completion\.\d+\.tool_calls\.`)
)

func normalizeSpan(sp otlpSpan, source string) (trace.Call, bool) {
	c := trace.Call{ID: sp.SpanID, Source: source}

	var (
		reqModel, respModel string
		taskByAlias         = map[string]string{}
		promptParts         = map[int]string{} // index -> content
		completionParts     = map[int]string{}
		inputMessages       string
		outputMessages      string
		promptRaw           string
		completionRaw       string
		finishReasons       string
		operation           string
		sawGenAI            bool
		sawToolCalls        bool
	)

	for _, kv := range sp.Attributes {
		if m := rePromptIdx.FindStringSubmatch(kv.Key); m != nil {
			sawGenAI = true
			if m[2] == "content" {
				i, _ := strconv.Atoi(m[1])
				promptParts[i] = kv.Value.asString()
			}
			continue
		}
		if reToolCallIdx.MatchString(kv.Key) {
			sawGenAI = true
			sawToolCalls = true
			continue
		}
		if m := reCompletionIdx.FindStringSubmatch(kv.Key); m != nil {
			sawGenAI = true
			i, _ := strconv.Atoi(m[1])
			switch m[2] {
			case "content":
				completionParts[i] = kv.Value.asString()
			case "finish_reason":
				if kv.Value.asString() == "tool_calls" {
					sawToolCalls = true
				}
			}
			continue
		}
		fld, ok := aliasTable[kv.Key]
		if !ok {
			continue
		}
		switch fld {
		case fProvider:
			c.Provider = kv.Value.asString()
			sawGenAI = true
		case fReqModel:
			reqModel = kv.Value.asString()
			sawGenAI = true
		case fRespModel:
			respModel = kv.Value.asString()
			sawGenAI = true
		case fInputTokens:
			if n, ok := kv.Value.asInt(); ok {
				c.InputTokens = n
				sawGenAI = true
			}
		case fOutputTokens:
			if n, ok := kv.Value.asInt(); ok {
				c.OutputTokens = n
				sawGenAI = true
			}
		case fOperation:
			operation = kv.Value.asString()
		case fTask:
			taskByAlias[kv.Key] = kv.Value.asString()
		case fErrorType:
			if kv.Value.asString() != "" {
				c.Error = true
			}
		case fInputMessages:
			inputMessages = kv.Value.asString()
		case fOutputMessages:
			outputMessages = kv.Value.asString()
		case fPromptRaw:
			promptRaw = kv.Value.asString()
		case fCompletionRaw:
			completionRaw = kv.Value.asString()
		case fFinishReasons:
			finishReasons = kv.Value.asString()
		}
	}

	if !sawGenAI {
		return trace.Call{}, false
	}

	// Model: response model wins (it is what was actually billed).
	c.Model = respModel
	if c.Model == "" {
		c.Model = reqModel
	}
	if c.Provider == "" {
		c.Provider = trace.GuessProvider(c.Model)
	}

	// Task hint precedence.
	for _, k := range taskAliases {
		if v := taskByAlias[k]; v != "" {
			c.Task = v
			break
		}
	}

	// Prompt precedence: indexed parts > structured messages > raw scalar.
	switch {
	case len(promptParts) > 0:
		c.Prompt = joinIndexed(promptParts)
	case inputMessages != "":
		c.Prompt = extractTextFromJSON([]byte(inputMessages))
	default:
		c.Prompt = promptRaw
	}
	switch {
	case len(completionParts) > 0:
		c.Output = joinIndexed(completionParts)
	case outputMessages != "":
		c.Output = extractTextFromJSON([]byte(outputMessages))
	default:
		c.Output = completionRaw
	}

	// Timestamps / latency.
	start := parseUnixNano(sp.StartTimeUnixNano)
	end := parseUnixNano(sp.EndTimeUnixNano)
	if !start.IsZero() {
		c.Timestamp = start
		if !end.IsZero() && end.After(start) {
			c.LatencyMS = float64(end.Sub(start)) / float64(time.Millisecond)
		}
	}

	// Errors: protojson enum string or numeric code 2.
	code := strings.Trim(string(sp.Status.Code), `"`)
	if code == "STATUS_CODE_ERROR" || code == "2" {
		c.Error = true
	}

	c.StructuredOutput = sawToolCalls ||
		strings.Contains(finishReasons, "tool_calls") ||
		operation == "execute_tool" ||
		looksLikeJSON(c.Output)
	return c, true
}

func joinIndexed(parts map[int]string) string {
	max := -1
	for i := range parts {
		if i > max {
			max = i
		}
	}
	var b strings.Builder
	for i := 0; i <= max; i++ {
		if s, ok := parts[i]; ok && s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(s)
		}
	}
	return b.String()
}
