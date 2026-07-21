export async function api(path, options = {}) {
  const response = await fetch(path, {
    credentials: 'include',
    headers: {
      ...(options.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
      ...(options.headers || {})
    },
    ...options
  })

  if (!response.ok) {
    const text = await response.text()
    const error = new Error(text || `HTTP ${response.status}`)
    error.status = response.status
    throw error
  }

  const contentType = response.headers.get('content-type') || ''
  if (!contentType.includes('application/json')) {
    return null
  }
  return response.json()
}

export function login(username, password) {
  return api('/api/login', {
    method: 'POST',
    body: JSON.stringify({ username, password })
  })
}

export function verifyLoginMFA(token, code) {
  return api('/api/login/mfa', {
    method: 'POST',
    body: JSON.stringify({ token, code })
  })
}

export function logout() {
  return api('/api/logout', { method: 'POST' })
}

export function me() {
  return api('/api/me')
}

export function getAppConfig() {
  return api('/api/app-config')
}

export function getSetupStatus() {
  return api('/api/setup/status')
}

export function completeSetup(payload) {
  return api('/api/setup', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function changeMyPassword(currentPassword, newPassword) {
  return api('/api/me/password', {
    method: 'PUT',
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword
    })
  })
}

export function listUsers({ page = 1, pageSize = 10, username = '' } = {}) {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize)
  })
  if (username.trim()) {
    query.set('username', username.trim())
  }
  return api(`/api/users?${query.toString()}`)
}

export function getSettings() {
  return api('/api/settings')
}

export function updateSettings(payload) {
  return api('/api/settings', {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function createUser(payload) {
  return api('/api/users', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}

export function updateUser(id, payload) {
  return api(`/api/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function resetUserPassword(id, password) {
  return api(`/api/users/${id}/password`, {
    method: 'PUT',
    body: JSON.stringify({ password })
  })
}

export function resetUserMFA(id) {
  return api(`/api/users/${id}/mfa/reset`, { method: 'POST' })
}

export function deleteUser(id) {
  return api(`/api/users/${id}`, { method: 'DELETE' })
}

export function getTree() {
  return api('/api/tree')
}

export function sortTree(tree) {
  return api('/api/tree/sort', {
    method: 'PUT',
    body: JSON.stringify({ tree })
  })
}

export function createFolder(parentId, name) {
  return api('/api/folders', {
    method: 'POST',
    body: JSON.stringify({ parent_id: parentId, name })
  })
}

export function renameFolder(id, name) {
  return api(`/api/folders/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name })
  })
}

export function deleteFolder(id) {
  return api(`/api/folders/${id}`, { method: 'DELETE' })
}

export function createDocument(folderId, title) {
  return api('/api/documents', {
    method: 'POST',
    body: JSON.stringify({ folder_id: folderId, title })
  })
}

export function importDocument(folderId, file) {
  const data = new FormData()
  data.append('folder_id', String(folderId))
  data.append('file', file)
  return api('/api/documents/import', {
    method: 'POST',
    body: data
  })
}

export function getDocument(id) {
  return api(`/api/documents/${id}`)
}

export function saveDocument(id, payload) {
  return api(`/api/documents/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload)
  })
}

export function listDocumentVersions(id) {
  return api(`/api/documents/${id}/versions`)
}

export function getDocumentVersion(id, versionId) {
  return api(`/api/documents/${id}/versions/${versionId}`)
}

export function restoreDocumentVersion(id, versionId) {
  return api(`/api/documents/${id}/versions/${versionId}/restore`, { method: 'POST' })
}

export function deleteDocument(id) {
  return api(`/api/documents/${id}`, { method: 'DELETE' })
}

export function listTrash({ page = 1, pageSize = 10 } = {}) {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize)
  })
  return api(`/api/trash?${query.toString()}`)
}

export function listOperationLogs({ page = 1, pageSize = 20, action = '', q = '' } = {}) {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize)
  })
  if (action) query.set('action', action)
  if (q.trim()) query.set('q', q.trim())
  return api(`/api/operation-logs?${query.toString()}`)
}

export function restoreTrashItem(type, id) {
  return api(`/api/trash/${type}/${id}/restore`, { method: 'POST' })
}

export function purgeTrashItem(type, id) {
  return api(`/api/trash/${type}/${id}`, { method: 'DELETE' })
}

export function searchDocuments(q) {
  return api(`/api/search?q=${encodeURIComponent(q)}`)
}

export function uploadFile(file) {
  const data = new FormData()
  data.append('file', file)
  return api('/api/uploads', {
    method: 'POST',
    body: data
  })
}
