import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Zap, Terminal, Code2, Settings2 } from 'lucide-react'
import { settings } from '../api/client'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8200'

interface CompressionOption {
  id: string
  label: string
  emoji: string
  savings: string
  desc: string
  detail: string
}

const COMPRESSION_OPTIONS: CompressionOption[] = [
  {
    id: 'off',
    label: 'Off',
    emoji: '⊘',
    savings: '0%',
    desc: 'No compression',
    detail: 'Passes messages to the model exactly as-is. Use when you need exact prompt control.',
  },
  {
    id: 'lite',
    label: 'Lite',
    emoji: '🪶',
    savings: '~15%',
    desc: 'Whitespace + zero-width cleanup',
    detail: 'Collapses blank lines, removes zero-width chars, trims trailing spaces. Safe for all content.',
  },
  {
    id: 'standard',
    label: 'Standard',
    emoji: '🪨',
    savings: '~30%',
    desc: 'Filler removal + decorations',
    detail: 'All Lite + strips AI filler phrases ("Certainly!", "I\'d be happy to…"), decorative separators, redundant markdown. Good default for coding assistants.',
  },
  {
    id: 'aggressive',
    label: 'Aggressive',
    emoji: '⚡',
    savings: '~50%',
    desc: 'Aging + tool result capping',
    detail: 'All Standard + old messages progressively condensed, tool/function outputs capped at 40 lines. Ideal for long sessions with many tool calls.',
  },
  {
    id: 'ultra',
    label: 'Ultra',
    emoji: '🔥',
    savings: '~75%',
    desc: 'Stopword removal + extreme aging',
    detail: 'All Aggressive + stopword removal from prose, very old messages trimmed to 200 chars. Use when tokens are scarce.',
  },
  {
    id: 'rtk',
    label: 'RTK',
    emoji: '🧰',
    savings: '60–90%',
    desc: 'Domain-aware: shell/test/build/git',
    detail: 'Detects output type and applies targeted filters: test runs → summary only, build logs → errors only, git log → first line per commit, stack traces → top 5 frames, shell output → dedup + last 80 lines.',
  },
  {
    id: 'stacked',
    label: 'Stacked',
    emoji: '🔗',
    savings: '78–95%',
    desc: 'RTK → Standard pipeline',
    detail: 'Runs RTK compression first (domain filters), then Standard (filler removal). Best for mixed prompts with tool logs and prose.',
  },
]

function CopyBtn({ text }: { text: string }) {
  const [ok, setOk] = useState(false)
  const copy = () => {
    navigator.clipboard.writeText(text)
    setOk(true)
    setTimeout(() => setOk(false), 1800)
  }
  return (
    <button className="setup-copy-btn" onClick={copy} title="Copy">
      {ok ? <Check size={13} /> : <Copy size={13} />}
    </button>
  )
}

function CodeBlock({ code, lang = '' }: { code: string; lang?: string }) {
  return (
    <div className="setup-code-block">
      {lang && <div className="setup-code-lang">{lang}</div>}
      <pre className="setup-code-pre"><code>{code.trim()}</code></pre>
      <CopyBtn text={code.trim()} />
    </div>
  )
}

export default function SetupPage() {
  const qc = useQueryClient()

  const { data: currentSettings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: settings.get,
  })

  const mutation = useMutation({
    mutationFn: (mode: string) => settings.put({ compression_mode: mode }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings'] }),
  })

  const activeMode = currentSettings?.compression_mode ?? 'standard'

  return (
    <div className="setup-page">
      <div className="setup-header">
        <Settings2 size={20} className="setup-header-icon" />
        <div>
          <h1 className="setup-title">Setup & Quick Start</h1>
          <p className="setup-subtitle">Connect any SDK to your AiRouter instance</p>
        </div>
      </div>

      {/* ── Endpoint ── */}
      <section className="setup-section">
        <div className="setup-section-title"><Zap size={14} /> Endpoint</div>
        <CodeBlock code={API_URL} />
      </section>

      {/* ── Compression mode ── */}
      <section className="setup-section">
        <div className="setup-section-title"><Zap size={14} /> Context Compression</div>
        <p className="setup-section-desc">
          Applied automatically to every request. Change takes effect within 15 seconds.
        </p>
        {isLoading ? (
          <div className="setup-loading">Loading…</div>
        ) : (
          <div className="setup-compression-grid">
            {COMPRESSION_OPTIONS.map(opt => (
              <button
                key={opt.id}
                className={`setup-compression-card ${activeMode === opt.id ? 'active' : ''} ${mutation.isPending ? 'disabled' : ''}`}
                onClick={() => mutation.mutate(opt.id)}
                disabled={mutation.isPending}
              >
                <div className="setup-comp-top">
                  <span className="setup-comp-emoji">{opt.emoji}</span>
                  <span className="setup-comp-label">{opt.label}</span>
                  <span className="setup-comp-savings">{opt.savings}</span>
                </div>
                <div className="setup-comp-desc">{opt.desc}</div>
                <div className="setup-comp-detail">{opt.detail}</div>
                {activeMode === opt.id && <div className="setup-comp-active-dot" />}
              </button>
            ))}
          </div>
        )}
      </section>

      {/* ── Claude Code CLI ── */}
      <section className="setup-section">
        <div className="setup-section-title"><Terminal size={14} /> Claude Code CLI</div>
        <CodeBlock lang="bash" code={`
ANTHROPIC_BASE_URL=${API_URL} \\
ANTHROPIC_API_KEY=<your-api-key> \\
claude
        `} />
        <p className="setup-section-desc">Or add to your shell profile:</p>
        <CodeBlock lang="bash" code={`
export ANTHROPIC_BASE_URL="${API_URL}"
export ANTHROPIC_API_KEY="<your-api-key>"
        `} />
      </section>

      {/* ── Anthropic SDK ── */}
      <section className="setup-section">
        <div className="setup-section-title"><Code2 size={14} /> Anthropic SDK</div>

        <p className="setup-section-desc" style={{ marginBottom: 8 }}>Python</p>
        <CodeBlock lang="python" code={`
import anthropic

client = anthropic.Anthropic(
    api_key="<your-api-key>",
    base_url="${API_URL}",
)

message = client.messages.create(
    model="claude-3-5-sonnet-20241022",   # auto-routed to latest
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello!"}],
)
print(message.content[0].text)
        `} />

        <p className="setup-section-desc" style={{ marginBottom: 8 }}>TypeScript / Node</p>
        <CodeBlock lang="typescript" code={`
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: "<your-api-key>",
  baseURL: "${API_URL}",
});

const msg = await client.messages.create({
  model: "claude-3-5-sonnet-20241022",   // auto-routed to latest
  max_tokens: 1024,
  messages: [{ role: "user", content: "Hello!" }],
});
console.log(msg.content[0].text);
        `} />
      </section>

      {/* ── OpenAI SDK ── */}
      <section className="setup-section">
        <div className="setup-section-title"><Code2 size={14} /> OpenAI SDK</div>

        <p className="setup-section-desc" style={{ marginBottom: 8 }}>Python</p>
        <CodeBlock lang="python" code={`
from openai import OpenAI

client = OpenAI(
    api_key="<your-api-key>",
    base_url="${API_URL}/v1",
)

response = client.chat.completions.create(
    model="gpt-4",                 # auto-routed to gpt-4.1
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response.choices[0].message.content)
        `} />

        <p className="setup-section-desc" style={{ marginBottom: 8 }}>TypeScript / Node</p>
        <CodeBlock lang="typescript" code={`
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "<your-api-key>",
  baseURL: "${API_URL}/v1",
});

const response = await client.chat.completions.create({
  model: "gpt-4",                 // auto-routed to gpt-4.1
  messages: [{ role: "user", content: "Hello!" }],
});
console.log(response.choices[0].message.content);
        `} />
      </section>

    </div>
  )
}
