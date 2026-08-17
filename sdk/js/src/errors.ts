import { ErrorCodes } from './types.js'

export class NodeAPIError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(code ? `${code}: ${message}` : message)
    this.name = 'NodeAPIError'
    this.status = status
    this.code = code
  }
}

export function isNodeAPIError(err: unknown): err is NodeAPIError {
  return err instanceof NodeAPIError
}

export function isQuotaExceeded(err: unknown): boolean {
  return isNodeAPIError(err) && err.code === ErrorCodes.quota_exceeded
}

export function isUnauthorized(err: unknown): boolean {
  if (!isNodeAPIError(err)) return false
  return (
    err.code === ErrorCodes.unauthorized ||
    err.code === ErrorCodes.invalid_credentials ||
    err.code === ErrorCodes.token_expired ||
    err.code === ErrorCodes.token_revoked
  )
}
