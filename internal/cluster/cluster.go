// Package cluster groups normalized calls into task clusters with
// deterministic heuristics — no embeddings, no network, reproducible
// across runs.
//
// Precedence:
//  1. explicit task/endpoint attribute  -> "task:<name>"
//  2. prompt template prefix hash       -> "tpl:<fnv64a of normalized prefix>"
//  3. model-only fallback               -> "model:<model>" (no task, no prompt)
package cluster

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"

	"github.com/Mindburn-Labs/distillscan/internal/trace"
)

// Cluster is a group of calls believed to be the same recurring task.
type Cluster struct {
	Key   string // stable id, see package comment
	Kind  string // "task" | "template" | "model"
	Label string // human label for reports
	Calls []trace.Call
}

// Slot replacement order matters: specific patterns before the generic
// digit-run rule, so a UUID never half-survives as ⟨NUM⟩ debris.
var (
	reURL   = regexp.MustCompile(`https?://\S+`)
	reEmail = regexp.MustCompile(`\b[\w.+-]+@[\w-]+(\.[\w-]+)+\b`)
	reUUID  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reDate  = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}(:\d{2})?(\.\d+)?Z?)?\b|\b\d{1,2}/\d{1,2}/\d{2,4}\b`)
	reHex   = regexp.MustCompile(`\b[0-9a-fA-F]{12,}\b`)
	reNum   = regexp.MustCompile(`\d{2,}([.,]\d+)*`)
	reWS    = regexp.MustCompile(`\s+`)
)

// templatePrefixLen bounds the prefix used for the cluster key. Long enough
// to separate real templates, short enough that trailing variable content
// (the document body, the user question) does not fragment clusters.
const templatePrefixLen = 160

// Template normalizes a prompt into its reusable skeleton: whitespace
// collapsed, case folded, and volatile spans (URLs, emails, UUIDs, dates,
// hex ids, digit runs) replaced with slot markers.
func Template(prompt string) string {
	t := prompt
	t = reURL.ReplaceAllString(t, "<url>")
	t = reEmail.ReplaceAllString(t, "<email>")
	t = reUUID.ReplaceAllString(t, "<uuid>")
	t = reDate.ReplaceAllString(t, "<date>")
	t = reHex.ReplaceAllString(t, "<hex>")
	t = reNum.ReplaceAllString(t, "<num>")
	t = reWS.ReplaceAllString(t, " ")
	return strings.ToLower(strings.TrimSpace(t))
}

// TemplateKey returns the stable cluster key for a prompt: fnv64a over the
// template's fixed-length prefix.
func TemplateKey(prompt string) string {
	return "tpl:" + PrefixTemplateHash(prompt)
}

// PrefixTemplateHash hashes the normalized template's fixed-length prefix —
// the instruction head that defines a task. Scoring reuses it for repetition
// (how many distinct instruction prefixes a cluster contains) and drift
// (how that prefix distribution shifts over time). Deliberately blind to
// variable payloads (documents, tickets) after the prefix: payload variety
// is fine for distillation; instruction churn is not.
func PrefixTemplateHash(prompt string) string {
	return hash(prefix(Template(prompt), templatePrefixLen))
}

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func hash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%016x", h.Sum64())
}

// Group buckets calls into clusters, deterministically ordered by key.
func Group(calls []trace.Call) []*Cluster {
	byKey := map[string]*Cluster{}
	for _, c := range calls {
		key, kind := keyFor(c)
		cl, ok := byKey[key]
		if !ok {
			cl = &Cluster{Key: key, Kind: kind}
			byKey[key] = cl
		}
		cl.Calls = append(cl.Calls, c)
	}
	out := make([]*Cluster, 0, len(byKey))
	for _, cl := range byKey {
		sort.SliceStable(cl.Calls, func(i, j int) bool {
			return cl.Calls[i].Timestamp.Before(cl.Calls[j].Timestamp)
		})
		cl.Label = labelFor(cl)
		out = append(out, cl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func keyFor(c trace.Call) (key, kind string) {
	if t := strings.TrimSpace(c.Task); t != "" {
		return "task:" + strings.ToLower(t), "task"
	}
	if strings.TrimSpace(c.Prompt) != "" {
		return TemplateKey(c.Prompt), "template"
	}
	m := c.Model
	if m == "" {
		m = "unknown"
	}
	return "model:" + strings.ToLower(m), "model"
}

func labelFor(cl *Cluster) string {
	switch cl.Kind {
	case "task":
		return strings.TrimPrefix(cl.Key, "task:")
	case "model":
		return "(no prompt) " + strings.TrimPrefix(cl.Key, "model:")
	default:
		// First words of the template, word-safe, as a human hint.
		t := Template(cl.Calls[0].Prompt)
		if len(t) > 48 {
			cut := strings.LastIndex(t[:48], " ")
			if cut < 24 {
				cut = 48
			}
			t = t[:cut] + "…"
		}
		return `"` + t + `"`
	}
}
