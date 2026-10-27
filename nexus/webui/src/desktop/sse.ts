import { getDesktopBridge, getWailsRuntime, isDesktopRuntime } from './bridge'

export type EventSourceLike = Pick<EventSource, 'onopen' | 'onmessage' | 'onerror' | 'readyState' | 'close'>

class AuthenticatedFetchEventSource implements EventSourceLike {
  onopen: EventSource['onopen'] = null
  onmessage: EventSource['onmessage'] = null
  onerror: EventSource['onerror'] = null
  readyState: number = EventSource.CONNECTING

  private closed = false
  private controller: AbortController | null = null
  private subscriptionId: string | null = null
  private eventName: string | null = null

  constructor(private readonly url: string) {
    void this.connectLoop()
  }

  private async connectLoop() {
    if (isDesktopRuntime()) {
      await this.connectDesktop()
      return
    }

    while (!this.closed) {
      const token = localStorage.getItem('token')
      if (!token) {
        this.readyState = EventSource.CLOSED
        this.emitError(new Error('Authentication is required to open this stream'))
        return
      }

      const controller = new AbortController()
      this.controller = controller
      try {
        const response = await fetch(this.url, {
          headers: {
            Accept: 'text/event-stream',
            Authorization: `Bearer ${token}`,
          },
          signal: controller.signal,
        })
        if (!response.ok || !response.body) {
          throw new Error(`SSE request failed with status ${response.status}`)
        }

        this.readyState = EventSource.OPEN
        this.onopen?.call(this as unknown as EventSource, new Event('open'))
        await this.readMessages(response.body, controller.signal)
      } catch (error) {
        if (this.closed || controller.signal.aborted) return
        this.readyState = EventSource.CONNECTING
        this.emitError(error)
      } finally {
        if (this.controller === controller) this.controller = null
      }

      if (!this.closed) await new Promise((resolve) => window.setTimeout(resolve, 3000))
    }
  }

  private async connectDesktop() {
    const token = localStorage.getItem('token')
    const bridge = getDesktopBridge()
    const runtime = getWailsRuntime()
    if (!token || !bridge || !runtime) {
      this.readyState = EventSource.CLOSED
      this.emitError(new Error('Authenticated desktop streaming is unavailable'))
      return
    }

    try {
      const subscription = await bridge.DesktopSSESubscribeAuthenticated(
        this.url,
        `Bearer ${token}`,
      )
      if (this.closed) {
        await bridge.DesktopSSEUnsubscribe(subscription.id)
        return
      }
      this.subscriptionId = subscription.id
      this.eventName = subscription.eventName
      runtime.EventsOn(this.eventName, (payload: unknown) => {
        if (this.closed) return
        const event = (payload || {}) as {
          type?: string
          data?: unknown
          error?: string
        }
        if (event.type === 'open') {
          this.readyState = EventSource.OPEN
          this.onopen?.call(this as unknown as EventSource, new Event('open'))
        } else if (event.type === 'message') {
          const message = new MessageEvent('message', { data: String(event.data ?? '') })
          this.onmessage?.call(this as unknown as EventSource, message)
        } else if (event.type === 'error') {
          this.readyState = EventSource.CONNECTING
          this.emitError(new Error(event.error || 'Desktop SSE request failed'))
        } else if (event.type === 'close') {
          this.close()
        }
      })
    } catch (error) {
      this.readyState = EventSource.CLOSED
      this.emitError(error)
    }
  }

  private async readMessages(body: ReadableStream<Uint8Array>, signal: AbortSignal) {
    const reader = body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let dataLines: string[] = []

    const dispatch = () => {
      if (dataLines.length) {
        const event = new MessageEvent('message', { data: dataLines.join('\n') })
        this.onmessage?.call(this as unknown as EventSource, event)
        dataLines = []
      }
    }

    try {
      while (!signal.aborted) {
        const { done, value } = await reader.read()
        buffer += decoder.decode(value, { stream: !done })
        const lines = buffer.split(/\r?\n/)
        buffer = lines.pop() || ''
        for (const line of lines) {
          if (!line) {
            dispatch()
          } else if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).replace(/^ /, ''))
          }
        }
        if (done) {
          if (buffer.startsWith('data:')) dataLines.push(buffer.slice(5).replace(/^ /, ''))
          dispatch()
          return
        }
      }
    } finally {
      if (signal.aborted) void reader.cancel().catch(() => undefined)
      reader.releaseLock()
    }
  }

  private emitError(error: unknown) {
    const event = new Event('error')
    Object.defineProperty(event, 'error', { value: error })
    this.onerror?.call(this as unknown as EventSource, event)
  }

  close() {
    if (this.closed) return
    this.closed = true
    this.readyState = EventSource.CLOSED
    this.controller?.abort()
    this.controller = null
    const runtime = getWailsRuntime()
    if (runtime && this.eventName) {
      try {
        runtime.EventsOff(this.eventName)
      } catch (error) {
        console.warn('desktop sse EventsOff failed:', error)
      }
    }
    const bridge = getDesktopBridge()
    if (bridge && this.subscriptionId) {
      void bridge.DesktopSSEUnsubscribe(this.subscriptionId)
    }
    this.subscriptionId = null
    this.eventName = null
  }
}

export const openSSE = (url: string): EventSourceLike => new AuthenticatedFetchEventSource(url)
