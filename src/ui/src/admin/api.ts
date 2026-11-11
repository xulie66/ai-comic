const TOKEN_KEY = 'ac_token';

export function readToken(): string {
  const fromQuery = new URLSearchParams(location.search).get('token');
  if (fromQuery) {
    localStorage.setItem(TOKEN_KEY, fromQuery);
    return fromQuery;
  }
  return localStorage.getItem(TOKEN_KEY) || '';
}

export function authHeaders(): Record<string, string> {
  const token = readToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request(method: string, path: string, body?: unknown) {
  const response = await fetch(`/api/admin/${path}`, {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const text = await response.text();
  let payload: any = {};
  try { payload = text ? JSON.parse(text) : {}; } catch { payload = { error: text.slice(0, 200) }; }
  if (!response.ok || payload.success === false) {
    throw new Error(payload.error || `HTTP ${response.status}`);
  }
  return payload;
}

export const api = {
  get: (path: string) => request('GET', path),
  post: (path: string, body: unknown = {}) => request('POST', path, body)
};

export function qs(params: Record<string, unknown>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    search.set(key, String(value));
  }
  const text = search.toString();
  return text ? `?${text}` : '';
}

export function fmtBytes(bytes?: number | null) {
  const value = Number(bytes || 0);
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let index = 0;
  let size = value;
  while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1; }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function fmtTime(value?: string | number | null) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export function fmtDuration(seconds?: number | null) {
  const total = Math.max(0, Math.round(Number(seconds || 0)));
  const day = Math.floor(total / 86400);
  const hour = Math.floor((total % 86400) / 3600);
  const minute = Math.floor((total % 3600) / 60);
  if (day) return `${day} 天 ${hour} 小时`;
  if (hour) return `${hour} 小时 ${minute} 分`;
  return `${minute} 分`;
}
