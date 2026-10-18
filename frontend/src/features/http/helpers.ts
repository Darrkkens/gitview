// Small helpers of the HTTP and Security views (no components).

import { routeHref } from '../../lib/router'

export const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'ANY']

export const ROUTE_TABS = ['overview', 'request', 'response', 'flow', 'source', 'evidence']

const METHOD_CLASS: Record<string, string> = {
  GET: 'text-m-get bg-m-get/12',
  POST: 'text-m-post bg-m-post/12',
  PUT: 'text-m-put bg-m-put/12',
  PATCH: 'text-m-patch bg-m-patch/12',
  DELETE: 'text-m-delete bg-m-delete/12',
  ANY: 'text-m-any bg-m-any/12',
}

/** methodClass: the colours of an HTTP method badge. */
export const methodClass = (m: string) => METHOD_CLASS[m] ?? 'text-m-other bg-m-other/12'

/** archHref: the element in the Architecture view. */
export const archHref = (id: string) => routeHref('architecture', { node: id })
