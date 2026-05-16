import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { client } from '../api/client'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

interface Message {
  role: 'user' | 'assistant' | 'system'
  content: string
}

interface ModelListResponse {
  models: string[]
}

function fetchModels(): Promise<ModelListResponse> {
  return client.get('/admin/models').then((r) => r.data)
}

export default function ChatPage() {
  const [model, setModel] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [showSystem, setShowSystem] = useState(false)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [temperature, setTemperature] = useState(0.7)
  const [maxTokens, setMaxTokens] = useState(1024)
  const abortRef = useRef<AbortController | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  const { data: modelData, isLoading: loadingModels } = useQuery({
    queryKey: ['admin-models'],
    queryFn: fetchModels,
    staleTime: 60_000,
  })

  // Set default model once loaded
  useEffect(() => {
    if (modelData?.models?.length && !model) {
      setModel(modelData.models[0])
    }
  }, [modelData, model])

  // Scroll to bottom on new content
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const stop = () => {
    abortRef.current?.abort()
    setStreaming(false)
  }

  const sendMessage = async () => {
    const text = input.trim()
    if (!text || streaming) return

    setInput('')
    textareaRef.current && (textareaRef.current.style.height = 'auto')

    const userMsg: Message = { role: 'user', content: text }
    const newMessages = [...messages, userMsg]
    setMessages(newMessages)
    setStreaming(true)

    // Prepare messages array with optional system prompt
    const apiMessages: Message[] = systemPrompt.trim()
      ? [{ role: 'system', content: systemPrompt.trim() }, ...newMessages]
      : newMessages

    const body = JSON.stringify({
      model,
      messages: apiMessages,
      stream: true,
      temperature,
      max_tokens: maxTokens,
    })

    abortRef.current = new AbortController()

    // Append empty assistant message we'll fill in
    setMessages((prev) => [...prev, { role: 'assistant', content: '' }])

    try {
      const resp = await fetch(`${API_URL}/admin/chat`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${localStorage.getItem('admin_token') ?? ''}`,
        },
        body,
        signal: abortRef.current.signal,
      })

      if (!resp.ok || !resp.body) {
        const errText = await resp.text()
        setMessages((prev) => {
          const copy = [...prev]
          copy[copy.length - 1] = { role: 'assistant', content: `Error ${resp.status}: ${errText}` }
          return copy
        })
        setStreaming(false)
        return
      }

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (!line.startsWith('data:')) continue
          const data = line.slice(5).trim()
          if (data === '[DONE]') break

          try {
            const parsed = JSON.parse(data)
            const delta = parsed?.choices?.[0]?.delta?.content ?? ''
            if (delta) {
              setMessages((prev) => {
                const copy = [...prev]
                copy[copy.length - 1] = {
                  role: 'assistant',
                  content: copy[copy.length - 1].content + delta,
                }
                return copy
              })
            }
          } catch {
            // skip malformed chunk
          }
        }
      }
    } catch (err: unknown) {
      if ((err as Error).name !== 'AbortError') {
        setMessages((prev) => {
          const copy = [...prev]
          copy[copy.length - 1] = {
            role: 'assistant',
            content: `Request failed: ${(err as Error).message}`,
          }
          return copy
        })
      }
    } finally {
      setStreaming(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  const autoResize = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value)
    e.target.style.height = 'auto'
    e.target.style.height = Math.min(e.target.scrollHeight, 160) + 'px'
  }

  const clearChat = () => {
    stop()
    setMessages([])
  }

  return (
    <div className="chat-page">
      {/* ── Toolbar ── */}
      <div className="chat-toolbar">
        <div className="chat-toolbar-left">
          <h2 className="page-title" style={{ marginBottom: 0 }}>Test Chat</h2>

          <div className="chat-select-wrap">
            <label className="chat-label">Model</label>
            {loadingModels ? (
              <span className="chat-label" style={{ opacity: 0.5 }}>Loading…</span>
            ) : (
              <select
                className="chat-select"
                value={model}
                onChange={(e) => setModel(e.target.value)}
              >
                {modelData?.models?.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            )}
          </div>

          <div className="chat-select-wrap">
            <label className="chat-label">Temp</label>
            <input
              type="number"
              className="chat-input-num"
              min={0} max={2} step={0.1}
              value={temperature}
              onChange={(e) => setTemperature(parseFloat(e.target.value))}
            />
          </div>

          <div className="chat-select-wrap">
            <label className="chat-label">Max tokens</label>
            <input
              type="number"
              className="chat-input-num"
              min={64} max={32768} step={64}
              value={maxTokens}
              onChange={(e) => setMaxTokens(parseInt(e.target.value))}
            />
          </div>

          <button
            className={`btn btn-sm btn-ghost ${showSystem ? 'btn-active' : ''}`}
            onClick={() => setShowSystem((v) => !v)}
          >
            System prompt
          </button>
        </div>

        <div className="chat-toolbar-right">
          {messages.length > 0 && (
            <button className="btn btn-sm btn-ghost" onClick={clearChat}>
              Clear
            </button>
          )}
        </div>
      </div>

      {/* ── System prompt ── */}
      {showSystem && (
        <div className="chat-system-wrap">
          <textarea
            className="chat-system-input"
            rows={3}
            placeholder="System prompt (optional)…"
            value={systemPrompt}
            onChange={(e) => setSystemPrompt(e.target.value)}
          />
        </div>
      )}

      {/* ── Messages ── */}
      <div className="chat-messages">
        {messages.length === 0 && (
          <div className="chat-empty">
            <div className="chat-empty-icon">💬</div>
            <p>Select a model and start chatting</p>
            <p className="chat-empty-sub">Shift+Enter for newline · Enter to send</p>
          </div>
        )}

        {messages.map((msg, i) => (
          <div key={i} className={`chat-msg chat-msg-${msg.role}`}>
            <div className="chat-msg-avatar">
              {msg.role === 'user' ? '👤' : '🤖'}
            </div>
            <div className="chat-msg-body">
              <div className="chat-msg-role">
                {msg.role === 'user' ? 'You' : model || 'Assistant'}
              </div>
              <div className="chat-msg-content">
                <MessageContent content={msg.content} />
                {streaming && i === messages.length - 1 && msg.role === 'assistant' && (
                  <span className="chat-cursor" />
                )}
              </div>
            </div>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      {/* ── Input ── */}
      <div className="chat-input-row">
        <textarea
          ref={textareaRef}
          className="chat-textarea"
          rows={1}
          placeholder={`Message ${model || '…'}`}
          value={input}
          onChange={autoResize}
          onKeyDown={handleKeyDown}
          disabled={streaming}
        />
        {streaming ? (
          <button className="btn btn-danger chat-send-btn" onClick={stop}>
            ■ Stop
          </button>
        ) : (
          <button
            className="btn btn-primary chat-send-btn"
            onClick={sendMessage}
            disabled={!input.trim() || !model}
          >
            Send
          </button>
        )}
      </div>
    </div>
  )
}

// Renders message content — handles code blocks with basic syntax highlight
function MessageContent({ content }: { content: string }) {
  if (!content) return null

  const parts = content.split(/(```[\s\S]*?```)/g)
  return (
    <>
      {parts.map((part, i) => {
        if (part.startsWith('```')) {
          const firstNewline = part.indexOf('\n')
          const lang = firstNewline > 3 ? part.slice(3, firstNewline).trim() : ''
          const code = firstNewline > 0 ? part.slice(firstNewline + 1, -3) : part.slice(3, -3)
          return (
            <div className="chat-code-block" key={i}>
              {lang && <div className="chat-code-lang">{lang}</div>}
              <pre><code>{code}</code></pre>
            </div>
          )
        }
        // Inline code and line breaks
        return (
          <span key={i}>
            {part.split(/(`[^`]+`)/g).map((chunk, j) =>
              chunk.startsWith('`') && chunk.endsWith('`') ? (
                <code className="chat-inline-code" key={j}>{chunk.slice(1, -1)}</code>
              ) : (
                <span key={j} style={{ whiteSpace: 'pre-wrap' }}>{chunk}</span>
              )
            )}
          </span>
        )
      })}
    </>
  )
}
