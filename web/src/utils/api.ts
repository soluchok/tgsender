// API utility with automatic 401 handling

const API_URL = import.meta.env.VITE_API_URL || '';

// Callback to handle unauthorized responses (set by AuthContext)
let onUnauthorized: (() => void) | null = null;

/**
 * Set the callback to be called when a 401 Unauthorized response is received.
 * This should be called by AuthContext to register the logout function.
 */
export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler;
}

/**
 * Wrapper around fetch that automatically handles 401 responses
 * by triggering the unauthorized handler (logout + redirect to login).
 */
export async function apiFetch(
  endpoint: string,
  options: RequestInit = {}
): Promise<Response> {
  const url = endpoint.startsWith('http') ? endpoint : `${API_URL}${endpoint}`;
  
  const response = await fetch(url, {
    ...options,
    credentials: 'include',
  });

  // Handle 401 Unauthorized - redirect to login
  if (response.status === 401) {
    if (onUnauthorized) {
      onUnauthorized();
    }
    // Still throw so the caller knows the request failed
    throw new UnauthorizedError('Session expired. Please log in again.');
  }

  return response;
}

/**
 * Custom error class for unauthorized responses
 */
export class UnauthorizedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'UnauthorizedError';
  }
}

/**
 * Helper to check if an error is an UnauthorizedError
 */
export function isUnauthorizedError(error: unknown): error is UnauthorizedError {
  return error instanceof UnauthorizedError;
}
