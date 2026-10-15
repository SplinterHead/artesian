import { ref, onMounted, onUnmounted } from 'vue';

export function useWebSocket(eventHandlers = {}) {
  const wsConnected = ref(false);
  let socket = null;
  let reconnectTimeout = null;
  let isClosedManually = false;

  const handlers = new Map();

  // Pre-register provided handlers
  for (const [type, fn] of Object.entries(eventHandlers)) {
    on(type, fn);
  }

  function on(eventType, callback) {
    if (!handlers.has(eventType)) {
      handlers.set(eventType, new Set());
    }
    handlers.get(eventType).add(callback);
    return () => off(eventType, callback);
  }

  function off(eventType, callback) {
    if (handlers.has(eventType)) {
      handlers.get(eventType).delete(callback);
    }
  }

  function dispatch(msg) {
    if (!msg || !msg.type) return;
    const callbacks = handlers.get(msg.type);
    if (callbacks) {
      for (const fn of callbacks) {
        try {
          fn(msg.data, msg);
        } catch (err) {
          console.error(`Error in WebSocket handler for ${msg.type}:`, err);
        }
      }
    }
  }

  function connect() {
    if (isClosedManually) return;
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
      return;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/v1/ws`;
    
    try {
      socket = new WebSocket(wsUrl);

      socket.onopen = () => {
        wsConnected.value = true;
      };

      socket.onclose = () => {
        wsConnected.value = false;
        if (!isClosedManually) {
          reconnectTimeout = setTimeout(connect, 2000);
        }
      };

      socket.onerror = (err) => {
        console.warn('[WebSocket] Connection error:', err);
      };

      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          dispatch(msg);
        } catch (e) {
          console.error('[WebSocket] Failed to parse message JSON:', e);
        }
      };
    } catch (e) {
      console.error('[WebSocket] Initialization failed:', e);
      if (!isClosedManually) {
        reconnectTimeout = setTimeout(connect, 2000);
      }
    }
  }

  function disconnect() {
    isClosedManually = true;
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    if (socket) {
      socket.close();
      socket = null;
    }
    wsConnected.value = false;
  }

  onMounted(() => {
    connect();
  });

  onUnmounted(() => {
    disconnect();
  });

  return {
    wsConnected,
    connect,
    disconnect,
    on,
    off,
  };
}
