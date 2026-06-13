export async function fetchJSON<T>(
  input: RequestInfo,
  init?: RequestInit,
  token?: string,
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(init?.body ? { "Content-Type": "application/json" } : {}),
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };

  const response = await fetch(input, {
    ...init,
    headers: {
      ...headers,
      ...(init?.headers ?? {}),
    },
  });

  const payload = (await response.json().catch(() => null)) as Record<
    string,
    unknown
  > | null;

  if (!response.ok) {
    const message =
      typeof payload?.error === "string"
        ? payload.error
        : `${response.status} ${response.statusText}`;
    throw new Error(message);
  }

  return payload as T;
}
