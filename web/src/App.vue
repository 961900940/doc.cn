<script setup>
import { computed, defineAsyncComponent, nextTick, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  changeMyPassword,
  createDocument,
  createFolder,
  createUser,
  deleteDocument,
  deleteFolder,
  deleteUser,
  getAppConfig,
  getDocument,
  getDocumentVersion,
  getSettings,
  getSetupStatus,
  getTree,
  completeSetup,
  importDocument,
  listDocumentVersions,
  listOperationLogs,
  listTrash,
  listUsers,
  login,
  logout,
  me,
  purgeTrashItem,
  renameFolder,
  restoreDocumentVersion,
  restoreTrashItem,
  resetUserMFA,
  resetUserPassword,
  saveDocument,
  searchDocuments,
  sortTree,
  updateUser,
  updateSettings,
  uploadFile,
  verifyLoginMFA
} from './api'
import {
  EDITOR_ENGINES,
  loadEditorEngine,
  saveEditorEngine
} from './editor/constants'
import DocOutline from './editor/DocOutline.vue'
import {
  loadOutlineVisible,
  parseOutline,
  saveOutlineVisible
} from './editor/outline'

const ByteMdEditor = defineAsyncComponent(() => import('./editor/ByteMdEditor.vue'))
const VditorEditor = defineAsyncComponent(() => import('./editor/VditorEditor.vue'))

const user = ref(null)
const setupNeeded = ref(false)
const setupChecking = ref(true)
const setupSaving = ref(false)
const setupError = ref('')
const setupForm = ref({
  app_name: 'Doc System',
  admin_nickname: '超级管理员',
  admin_password: '',
  admin_password_confirm: '',
  force_password_change_new_users: false,
  jwt_expire_days: 1
})
const loginForm = ref({ username: 'admin', password: '' })
const loginError = ref('')
const mfaChallenge = ref(null)
const mfaForm = ref({ code: '' })
const loading = ref(false)
const loggingOut = ref(false)
const users = ref([])
const usersLoading = ref(false)
const usersTotal = ref(0)
const userPage = ref(1)
const userPageSize = ref(10)
const userSearchUsername = ref('')
const appConfig = ref({ app_name: 'Doc System' })
const settings = ref({
  app_name: 'Doc System',
  force_password_change_new_users: false,
  mfa_failed_window_seconds: 120,
  mfa_failed_max_attempts: 5,
  jwt_expire_days: 1
})
const settingsSaving = ref(false)
const userDialogVisible = ref(false)
const projectConfigDialogVisible = ref(false)
const trashDialogVisible = ref(false)
const trashItems = ref([])
const trashLoading = ref(false)
const trashTotal = ref(0)
const trashPage = ref(1)
const trashPageSize = ref(10)
const historyDialogVisible = ref(false)
const historyItems = ref([])
const historyLoading = ref(false)
const historyPreviewLoading = ref(false)
const historyRestoring = ref(false)
const selectedHistoryId = ref(null)
const historyPreview = ref(null)
const logsDialogVisible = ref(false)
const logsLoading = ref(false)
const logItems = ref([])
const logActions = ref([])
const logsTotal = ref(0)
const logsPage = ref(1)
const logsPageSize = ref(20)
const logsActionFilter = ref('')
const logsKeyword = ref('')
const logsScopeAll = ref(false)
const userFormVisible = ref(false)
const userSaving = ref(false)
const editingUser = ref(null)
const userForm = ref({ username: '', password: '', nickname: '', role: 'editor' })
const passwordDialogVisible = ref(false)
const passwordSaving = ref(false)
const passwordUser = ref(null)
const passwordForm = ref({ password: '' })
const myPasswordDialogVisible = ref(false)
const myPasswordSaving = ref(false)
const myPasswordForm = ref({ current_password: '', new_password: '', confirm_password: '' })
const tree = ref([])
const activeNode = ref(null)
const document = ref(null)
const editorMode = ref('split')
const editorModes = [
  { label: '编辑', value: 'edit' },
  { label: '分屏', value: 'split' },
  { label: '预览', value: 'preview' }
]
const previewWidth = ref('default')
const previewWidths = [
  { label: '窄屏', value: 'narrow' },
  { label: '默认', value: 'default' },
  { label: '宽屏', value: 'wide' }
]
const editorEngine = ref(loadEditorEngine())
const editorEngineOptions = Object.values(EDITOR_ENGINES)
const outlineVisible = ref(loadOutlineVisible())
const activeOutlineId = ref('')
const searchQuery = ref('')
const searchResults = ref([])
const searchLoading = ref(false)
const searchCompleted = ref(false)
const saving = ref(false)
const fileInput = ref(null)
const mfaCodeInput = ref(null)
const activeEditorRef = ref(null)

const appName = computed(() => appConfig.value.app_name || 'Doc System')
const canEdit = computed(() => user.value?.role === 'admin' || user.value?.role === 'editor')
const isSuperAdmin = computed(() => user.value?.username === 'admin' && user.value?.role === 'admin')
const effectiveEditorMode = computed(() => (canEdit.value ? editorMode.value : 'preview'))
const currentEditorEngineMeta = computed(
  () => EDITOR_ENGINES[editorEngine.value] || EDITOR_ENGINES.bytemd
)
const outlineItems = computed(() => parseOutline(document.value?.content || ''))
const showFolderDashboard = computed(
  () => !document.value && activeNode.value?.type === 'folder'
)
const folderChildren = computed(() => {
  const children = activeNode.value?.children || []
  return children
})
const folderDocuments = computed(() =>
  folderChildren.value.filter((item) => item.type === 'document')
)
const folderSubfolders = computed(() =>
  folderChildren.value.filter((item) => item.type === 'folder')
)

function setEditorEngine(engine) {
  editorEngine.value = saveEditorEngine(engine)
  ElMessage.success(`已切换为${EDITOR_ENGINES[editorEngine.value].label}`)
}

function toggleOutline() {
  outlineVisible.value = !outlineVisible.value
  saveOutlineVisible(outlineVisible.value)
}

function handleOutlineSelect(item) {
  activeOutlineId.value = item.id
  activeEditorRef.value?.scrollToHeading?.(item)
}

onMounted(async () => {
  await loadAppConfig()
  try {
    const status = await getSetupStatus()
    setupNeeded.value = !!status?.needed
    if (status?.app_name) {
      setupForm.value.app_name = status.app_name
      appConfig.value = { app_name: status.app_name }
    }
  } catch {
    setupNeeded.value = false
  } finally {
    setupChecking.value = false
  }
  if (setupNeeded.value) return
  try {
    user.value = await me()
    if (!user.value.must_change_password) {
      await refreshTree(true)
    }
  } catch {
    user.value = null
  }
})

async function submitSetup() {
  setupError.value = ''
  const password = setupForm.value.admin_password
  const confirm = setupForm.value.admin_password_confirm
  if (!setupForm.value.app_name.trim()) {
    setupError.value = '项目名称不能为空'
    return
  }
  if (password !== confirm) {
    setupError.value = '两次输入的管理员密码不一致'
    return
  }
  setupSaving.value = true
  try {
    const result = await completeSetup({
      app_name: setupForm.value.app_name.trim(),
      admin_nickname: setupForm.value.admin_nickname.trim() || '超级管理员',
      admin_password: password,
      force_password_change_new_users: setupForm.value.force_password_change_new_users,
      jwt_expire_days: setupForm.value.jwt_expire_days
    })
    setupNeeded.value = false
    appConfig.value = { app_name: setupForm.value.app_name.trim() }
    await finishLogin(result)
    ElMessage.success('初始化完成，已自动登录')
  } catch (error) {
    setupError.value = cleanError(error.message) || '初始化失败'
  } finally {
    setupSaving.value = false
  }
}

async function finishLogin(nextUser) {
  user.value = nextUser
  mfaChallenge.value = null
  mfaForm.value = { code: '' }
  if (!nextUser.must_change_password) {
    await refreshTree(true)
  }
}

async function focusMFAInput() {
  await nextTick()
  mfaCodeInput.value?.focus?.()
}

async function submitLogin() {
  loginError.value = ''
  loading.value = true
  try {
    const result = await login(loginForm.value.username, loginForm.value.password)
    if (result.mfa_required) {
      mfaChallenge.value = result
      mfaForm.value = { code: '' }
      ElMessage.success(result.must_bind_mfa ? '请先绑定 MFA' : '请输入 MFA 验证码')
      await focusMFAInput()
      return
    }
    await finishLogin(result)
    ElMessage.success(`欢迎回来，${result.nickname || result.username}`)
  } catch (error) {
    loginError.value = cleanError(error.message)
    ElMessage.error(loginError.value || '登录失败')
  } finally {
    loading.value = false
  }
}

async function submitLoginMFA() {
  if (loading.value) return
  if (!/^\d{6}$/.test(mfaForm.value.code)) {
    ElMessage.error('请输入 6 位验证码')
    await focusMFAInput()
    return
  }
  loading.value = true
  try {
    const result = await verifyLoginMFA(mfaChallenge.value.mfa_token, mfaForm.value.code)
    await finishLogin(result)
    ElMessage.success(`欢迎回来，${result.nickname || result.username}`)
  } catch (error) {
    const message = cleanError(error.message)
    ElMessage.error(message)
    mfaForm.value = { code: '' }
    if (error.status === 429) {
      backToPasswordLogin()
    } else {
      await focusMFAInput()
    }
  } finally {
    loading.value = false
  }
}

async function handleMFAInput(value) {
  const code = value.replace(/\D/g, '').slice(0, 6)
  if (code !== value) {
    mfaForm.value.code = code
  }
  if (code.length === 6) {
    await submitLoginMFA()
  }
}

function backToPasswordLogin() {
  mfaChallenge.value = null
  mfaForm.value = { code: '' }
}

function clearAppStateAfterLogout() {
  user.value = null
  document.value = null
  activeNode.value = null
  tree.value = []
  searchResults.value = []
  searchQuery.value = ''
  searchCompleted.value = false
  mfaChallenge.value = null
}

async function signOut() {
  try {
    await ElMessageBox.confirm('确定退出当前账号吗？', '退出登录', {
      confirmButtonText: '退出',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch {
    return
  }
  loggingOut.value = true
  try {
    await logout()
    clearAppStateAfterLogout()
    ElMessage.success('已退出登录')
  } finally {
    loggingOut.value = false
  }
}

async function openUserManager() {
  userDialogVisible.value = true
  userPage.value = 1
  await loadUsers()
}

async function openOperationLogs() {
  logsDialogVisible.value = true
  logsPage.value = 1
  await loadOperationLogs()
}

async function loadOperationLogs() {
  logsLoading.value = true
  try {
    const result = await listOperationLogs({
      page: logsPage.value,
      pageSize: logsPageSize.value,
      action: logsActionFilter.value,
      q: logsKeyword.value
    })
    logItems.value = result?.items || []
    logActions.value = result?.actions || logActions.value
    logsTotal.value = result?.total || 0
    logsPage.value = result?.page || logsPage.value
    logsPageSize.value = result?.page_size || logsPageSize.value
    logsScopeAll.value = !!result?.scope?.all
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '加载操作日志失败')
  } finally {
    logsLoading.value = false
  }
}

async function handleLogsPageChange(page) {
  logsPage.value = page
  await loadOperationLogs()
}

async function handleLogsPageSizeChange(size) {
  logsPageSize.value = size
  logsPage.value = 1
  await loadOperationLogs()
}

async function searchOperationLogs() {
  logsPage.value = 1
  await loadOperationLogs()
}

async function clearOperationLogSearch() {
  logsKeyword.value = ''
  logsActionFilter.value = ''
  logsPage.value = 1
  await loadOperationLogs()
}

function logOperatorLabel(row) {
  if (!row?.user_id) return '系统 / 未知'
  if (row.nickname) return `${row.nickname}（${row.username}）`
  return row.username || `用户 #${row.user_id}`
}

async function openProjectConfig() {
  projectConfigDialogVisible.value = true
  await loadSettings()
}

async function loadAppConfig() {
  try {
    appConfig.value = await getAppConfig()
  } catch {
    appConfig.value = { app_name: 'Doc System' }
  }
}

async function loadUsers() {
  usersLoading.value = true
  try {
    const result = await listUsers({
      page: userPage.value,
      pageSize: userPageSize.value,
      username: userSearchUsername.value
    })
    users.value = result.items || []
    usersTotal.value = result.total || 0
    userPage.value = result.page || userPage.value
    userPageSize.value = result.page_size || userPageSize.value
  } finally {
    usersLoading.value = false
  }
}

async function handleUserPageChange(page) {
  userPage.value = page
  await loadUsers()
}

async function handleUserPageSizeChange(size) {
  userPageSize.value = size
  userPage.value = 1
  await loadUsers()
}

async function searchUsers() {
  userPage.value = 1
  await loadUsers()
}

async function clearUserSearch() {
  userSearchUsername.value = ''
  userPage.value = 1
  await loadUsers()
}

async function loadSettings() {
  settings.value = {
    ...settings.value,
    ...(await getSettings())
  }
  appConfig.value = { app_name: settings.value.app_name || 'Doc System' }
}

async function saveAppNameSetting() {
  const appNameValue = settings.value.app_name.trim()
  if (!appNameValue) {
    ElMessage.error('项目名称不能为空')
    return
  }
  settingsSaving.value = true
  try {
    await updateSettings({ app_name: appNameValue })
    settings.value.app_name = appNameValue
    appConfig.value = { app_name: appNameValue }
    ElMessage.success('项目名称已更新')
  } catch (error) {
    ElMessage.error(cleanError(error.message))
    await loadSettings()
  } finally {
    settingsSaving.value = false
  }
}

async function saveForcePasswordSetting(value) {
  settingsSaving.value = true
  try {
    await updateSettings({ force_password_change_new_users: value })
    settings.value.force_password_change_new_users = value
    ElMessage.success(value ? '已开启新用户首次登录强制改密' : '已关闭新用户首次登录强制改密')
  } catch (error) {
    settings.value.force_password_change_new_users = !value
    ElMessage.error(cleanError(error.message))
  } finally {
    settingsSaving.value = false
  }
}

async function saveMFAFailureSettings() {
  settingsSaving.value = true
  try {
    await updateSettings({
      mfa_failed_window_seconds: settings.value.mfa_failed_window_seconds,
      mfa_failed_max_attempts: settings.value.mfa_failed_max_attempts
    })
    ElMessage.success('MFA 失败限制已更新')
  } catch (error) {
    ElMessage.error(cleanError(error.message))
    await loadSettings()
  } finally {
    settingsSaving.value = false
  }
}

async function saveJWTExpireDays() {
  settingsSaving.value = true
  try {
    await updateSettings({
      jwt_expire_days: settings.value.jwt_expire_days
    })
    ElMessage.success('JWT 有效期已更新，对新登录生效')
  } catch (error) {
    ElMessage.error(cleanError(error.message))
    await loadSettings()
  } finally {
    settingsSaving.value = false
  }
}

function openCreateUser() {
  editingUser.value = null
  userForm.value = { username: '', password: '', nickname: '', role: 'editor' }
  userFormVisible.value = true
}

function openEditUser(row) {
  editingUser.value = row
  userForm.value = {
    username: row.username,
    password: '',
    nickname: row.nickname,
    role: row.role
  }
  userFormVisible.value = true
}

async function submitUserForm() {
  userSaving.value = true
  try {
    if (editingUser.value) {
      await updateUser(editingUser.value.id, {
        nickname: userForm.value.nickname,
        role: userForm.value.role,
        mfa_enabled: editingUser.value.mfa_enabled
      })
      ElMessage.success('用户已更新')
    } else {
      await createUser(userForm.value)
      userPage.value = Math.max(1, Math.ceil((usersTotal.value + 1) / userPageSize.value))
      ElMessage.success('用户已创建')
    }
    userFormVisible.value = false
    await loadUsers()
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  } finally {
    userSaving.value = false
  }
}

async function toggleUserMFA(row, value) {
  const previous = !value
  try {
    await updateUser(row.id, {
      nickname: row.nickname,
      role: row.role,
      mfa_enabled: value
    })
    ElMessage.success(value ? '已开启 MFA' : '已关闭 MFA')
    await loadUsers()
  } catch (error) {
    row.mfa_enabled = previous
    ElMessage.error(cleanError(error.message))
  }
}

async function resetMFA(row) {
  try {
    await ElMessageBox.confirm(`确定重置用户“${row.username}”的 MFA 吗？重置后下次登录需要重新扫码绑定。`, '重置 MFA', {
      confirmButtonText: '重置',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await resetUserMFA(row.id)
    ElMessage.success('MFA 已重置')
    await loadUsers()
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  }
}

function openResetPassword(row) {
  passwordUser.value = row
  passwordForm.value = { password: '' }
  passwordDialogVisible.value = true
}

function randomItem(items) {
  const values = new Uint32Array(1)
  crypto.getRandomValues(values)
  return items[values[0] % items.length]
}

function shuffleText(value) {
  const chars = value.split('')
  for (let i = chars.length - 1; i > 0; i--) {
    const values = new Uint32Array(1)
    crypto.getRandomValues(values)
    const j = values[0] % (i + 1)
    ;[chars[i], chars[j]] = [chars[j], chars[i]]
  }
  return chars.join('')
}

function createPassword() {
  const letters = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ'
  const numbers = '23456789'
  const specials = '!@#$%^&*'
  const all = `${letters}${numbers}${specials}`
  const required = [
    randomItem(letters),
    randomItem(numbers),
    randomItem(specials)
  ]
  while (required.length < 12) {
    required.push(randomItem(all))
  }
  passwordForm.value.password = shuffleText(required.join(''))
}

function openMyPasswordDialog() {
  myPasswordForm.value = { current_password: '', new_password: '', confirm_password: '' }
  myPasswordDialogVisible.value = true
}

function selfPasswordError(password) {
  if (password.length < 8) {
    return '新密码至少 8 位'
  }
  const categories = [
    /\p{L}/u.test(password),
    /\p{N}/u.test(password),
    /[^\p{L}\p{N}\s]/u.test(password)
  ].filter(Boolean).length
  if (categories < 2) {
    return '新密码需包含字母、数字、特殊符号中的至少 2 种'
  }
  return ''
}

async function submitMyPasswordChange() {
  if (!myPasswordForm.value.current_password) {
    ElMessage.error('请输入当前密码')
    return
  }
  const passwordError = selfPasswordError(myPasswordForm.value.new_password)
  if (passwordError) {
    ElMessage.error(passwordError)
    return
  }
  if (myPasswordForm.value.new_password === myPasswordForm.value.current_password) {
    ElMessage.error('新密码不能和当前密码一致')
    return
  }
  if (myPasswordForm.value.new_password !== myPasswordForm.value.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }
  myPasswordSaving.value = true
  try {
    const username = user.value?.username || ''
    await changeMyPassword(myPasswordForm.value.current_password, myPasswordForm.value.new_password)
    ElMessage.success('密码已修改，请重新登录')
    myPasswordDialogVisible.value = false
    loginForm.value = { username, password: '' }
    clearAppStateAfterLogout()
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  } finally {
    myPasswordSaving.value = false
  }
}

async function submitForcedPasswordChange() {
  await submitMyPasswordChange()
}

async function submitPasswordReset() {
  if (!passwordUser.value) return
  if (!passwordForm.value.password) {
    ElMessage.error('新密码不能为空')
    return
  }
  passwordSaving.value = true
  try {
    await resetUserPassword(passwordUser.value.id, passwordForm.value.password)
    ElMessage.success('密码已重置')
    passwordDialogVisible.value = false
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  } finally {
    passwordSaving.value = false
  }
}

async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }
  const input = document.createElement('textarea')
  input.value = text
  input.setAttribute('readonly', '')
  input.style.position = 'fixed'
  input.style.left = '-9999px'
  document.body.appendChild(input)
  input.select()
  const ok = document.execCommand('copy')
  document.body.removeChild(input)
  if (!ok) {
    throw new Error('复制失败')
  }
}

async function copyPasswordAndReset() {
  if (!passwordForm.value.password) {
    ElMessage.error('新密码不能为空')
    return
  }
  if (!passwordUser.value) return
  passwordSaving.value = true
  try {
    await resetUserPassword(passwordUser.value.id, passwordForm.value.password)
    await copyText(passwordForm.value.password)
    ElMessage.success('密码已重置并复制')
    passwordDialogVisible.value = false
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  } finally {
    passwordSaving.value = false
  }
}

async function removeUser(row) {
  try {
    await ElMessageBox.confirm(`确定删除用户“${row.username}”吗？`, '删除用户', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await deleteUser(row.id)
    if (users.value.length === 1 && userPage.value > 1) {
      userPage.value -= 1
    }
    ElMessage.success('用户已删除')
    await loadUsers()
  } catch (error) {
    ElMessage.error(cleanError(error.message))
  }
}

async function refreshTree(selectRoot = false) {
  tree.value = await getTree()
  if (selectRoot || !activeNode.value) {
    activeNode.value = tree.value[0] || null
    return
  }
  const current = activeNode.value
  const found = findTreeNode(tree.value, current.type, current.id)
  activeNode.value = found || tree.value[0] || null
}

function findTreeNode(nodes, type, id) {
  for (const node of nodes || []) {
    if (node.type === type && node.id === id) return node
    const child = findTreeNode(node.children || [], type, id)
    if (child) return child
  }
  return null
}

async function createRootFolder() {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能创建文件夹')
    return
  }
  const name = window.prompt('文件夹名称')
  if (!name) return
  await createFolder(0, name)
  await refreshTree()
}

async function createChildFolder(node) {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能创建文件夹')
    return
  }
  const name = window.prompt('文件夹名称')
  if (!name) return
  await createFolder(node.id, name)
  await refreshTree()
}

async function createDoc(node = null) {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能创建文档')
    return
  }
  const title = window.prompt('文档标题')
  if (!title) return
  const folderId = node?.type === 'folder' ? node.id : 0
  const result = await createDocument(folderId, title)
  await refreshTree()
  await openDocument({ id: result.id, type: 'document', title })
}

function importFileToFolder(node = null) {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能导入文件')
    return
  }
  const folderId = node?.type === 'folder' ? node.id : 0
  const input = window.document.createElement('input')
  input.type = 'file'
  input.accept = '.md,.markdown,.txt,.log,.csv,.html,.htm,.docx,.doc,.pdf,.xls,.xlsx'
  input.onchange = async () => {
    const file = input.files?.[0]
    if (!file) return
    try {
      const result = await importDocument(folderId, file)
      ElMessage.success(result?.message || '文件已导入')
      await refreshTree()
      await openDocument({ id: result.id, type: 'document', title: result.title })
    } catch (error) {
      ElMessage.error(cleanError(error.message) || '导入失败')
    }
  }
  input.click()
}

async function renameNode(node) {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能重命名')
    return
  }
  if (node.type === 'root') return
  const name = window.prompt('新名称', node.title)
  if (!name || name === node.title) return
  if (node.type === 'folder') {
    await renameFolder(node.id, name)
  } else if (document.value?.id === node.id) {
    document.value.title = name
    await saveCurrent()
  }
  await refreshTree()
}

async function removeNode(node) {
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能删除')
    return
  }
  if (node.type === 'root') return
  const impact = describeDeleteImpact(node)
  try {
    await ElMessageBox.confirm(
      impact.message,
      impact.title,
      {
        confirmButtonText: '移入回收站',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
  } catch {
    return
  }
  try {
    const result = node.type === 'folder'
      ? await deleteFolder(node.id)
      : await deleteDocument(node.id)
    if (node.type === 'document' && document.value?.id === node.id) {
      document.value = null
    }
    ElMessage.success(result?.message || '已移入回收站')
    await refreshTree()
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '删除失败')
  }
}

function describeDeleteImpact(node) {
  if (node.type === 'document') {
    return {
      title: '删除文档',
      message: `确定将文档“${node.title}”移入回收站吗？可以在回收站中恢复。`
    }
  }
  const stats = countNodeChildren(node)
  const parts = []
  if (stats.folders) parts.push(`${stats.folders} 个子文件夹`)
  if (stats.documents) parts.push(`${stats.documents} 篇文档`)
  const detail = parts.length ? `该文件夹下的 ${parts.join('、')} 也会一起移入回收站。` : '该文件夹当前没有子内容。'
  return {
    title: '删除文件夹',
    message: `确定将文件夹“${node.title}”移入回收站吗？${detail} 可以在回收站中恢复。`
  }
}

function countNodeChildren(node) {
  const stats = { folders: 0, documents: 0 }
  for (const child of node.children || []) {
    if (child.type === 'folder') {
      stats.folders += 1
      const nested = countNodeChildren(child)
      stats.folders += nested.folders
      stats.documents += nested.documents
    } else if (child.type === 'document') {
      stats.documents += 1
    }
  }
  return stats
}

async function openTrashDialog() {
  trashDialogVisible.value = true
  trashPage.value = 1
  await loadTrashItems()
}

async function loadTrashItems() {
  trashLoading.value = true
  try {
    const result = await listTrash({ page: trashPage.value, pageSize: trashPageSize.value })
    trashItems.value = result.items || []
    trashTotal.value = result.total || 0
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '加载回收站失败')
  } finally {
    trashLoading.value = false
  }
}

async function restoreTrash(row) {
  try {
    const result = await restoreTrashItem(row.type, row.id)
    ElMessage.success(result?.message || '已恢复')
    await reloadTrashAfterMutation()
    await refreshTree()
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '恢复失败')
  }
}

async function purgeTrash(row) {
  const label = row.type === 'folder' ? '文件夹' : '文档'
  try {
    await ElMessageBox.confirm(
      `确定永久删除${label}“${row.title}”吗？该操作不可恢复。`,
      `永久删除${label}`,
      {
        confirmButtonText: '永久删除',
        cancelButtonText: '取消',
        type: 'error'
      }
    )
  } catch {
    return
  }
  try {
    const result = await purgeTrashItem(row.type, row.id)
    ElMessage.success(result?.message || '已永久删除')
    await reloadTrashAfterMutation()
    await refreshTree()
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '永久删除失败')
  }
}

function trashImpact(row) {
  if (row.type === 'document') return '1 篇文档'
  const parts = []
  if (row.folder_count) parts.push(`${row.folder_count} 个文件夹`)
  if (row.document_count) parts.push(`${row.document_count} 篇文档`)
  return parts.join('、') || '空文件夹'
}

function formatBeijingTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  }).format(date).replace(/\//g, '-')
}

async function reloadTrashAfterMutation() {
  if (trashItems.value.length === 1 && trashPage.value > 1) {
    trashPage.value -= 1
  }
  await loadTrashItems()
}

async function handleTrashPageChange(page) {
  trashPage.value = page
  await loadTrashItems()
}

async function handleTrashPageSizeChange(size) {
  trashPageSize.value = size
  trashPage.value = 1
  await loadTrashItems()
}

async function handleNodeClick(node) {
  activeNode.value = node
  if (node.type === 'document') {
    await openDocument(node)
  } else {
    document.value = null
  }
}

function allowTreeDrop(draggingNode, dropNode, type) {
  if (!canEdit.value) return false
  const dragging = draggingNode.data
  const target = dropNode.data
  if (dragging.type === 'root') return false
  if (type === 'inner') {
    return target.type === 'root' || target.type === 'folder'
  }
  return target.type !== 'root'
}

async function handleTreeDrop() {
  if (!canEdit.value) return
  await sortTree(serializeTree(tree.value))
  await refreshTree()
}

function serializeTree(nodes) {
  return nodes.map((node) => ({
    id: node.id,
    type: node.type,
    children: serializeTree(node.children || [])
  }))
}

async function openDocument(node) {
  document.value = await getDocument(node.id)
  activeOutlineId.value = ''
  if (!canEdit.value) {
    editorMode.value = 'preview'
  }
  await nextTick()
}

async function saveCurrent() {
  if (!document.value) return
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能保存文档')
    return
  }
  if (saving.value) return
  saving.value = true
  try {
    await saveDocument(document.value.id, {
      title: document.value.title,
      content: document.value.content
    })
    await refreshTree()
    ElMessage.success('已保存')
  } finally {
    saving.value = false
  }
}

async function openHistoryDialog() {
  if (!document.value) return
  historyDialogVisible.value = true
  selectedHistoryId.value = null
  historyPreview.value = null
  await loadHistoryItems()
}

async function loadHistoryItems() {
  if (!document.value) return
  historyLoading.value = true
  try {
    const result = await listDocumentVersions(document.value.id)
    historyItems.value = result?.items || []
    if (
      selectedHistoryId.value &&
      !historyItems.value.some((item) => item.id === selectedHistoryId.value)
    ) {
      selectedHistoryId.value = null
      historyPreview.value = null
    }
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '加载历史版本失败')
  } finally {
    historyLoading.value = false
  }
}

async function selectHistoryVersion(row) {
  if (!document.value || !row) return
  selectedHistoryId.value = row.id
  historyPreviewLoading.value = true
  try {
    historyPreview.value = await getDocumentVersion(document.value.id, row.id)
  } catch (error) {
    historyPreview.value = null
    ElMessage.error(cleanError(error.message) || '加载历史内容失败')
  } finally {
    historyPreviewLoading.value = false
  }
}

async function restoreHistoryVersion() {
  if (!document.value || !selectedHistoryId.value || !canEdit.value) return
  try {
    await ElMessageBox.confirm(
      '恢复后当前正文会先保存为新的历史版本，再用所选版本覆盖。确定继续吗？',
      '恢复历史版本',
      {
        type: 'warning',
        confirmButtonText: '恢复',
        cancelButtonText: '取消'
      }
    )
  } catch {
    return
  }
  historyRestoring.value = true
  try {
    await restoreDocumentVersion(document.value.id, selectedHistoryId.value)
    document.value = await getDocument(document.value.id)
    await refreshTree()
    await loadHistoryItems()
    ElMessage.success('已恢复到所选历史版本')
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '恢复失败')
  } finally {
    historyRestoring.value = false
  }
}

async function runSearch() {
  const q = searchQuery.value.trim()
  if (!q) {
    searchResults.value = []
    searchCompleted.value = false
    return
  }
  searchLoading.value = true
  try {
    searchResults.value = await searchDocuments(q)
    searchCompleted.value = true
  } catch (error) {
    ElMessage.error(cleanError(error.message) || '搜索失败')
  } finally {
    searchLoading.value = false
  }
}

function clearDocumentSearch() {
  searchResults.value = []
  searchCompleted.value = false
}

async function insertUpload(event) {
  const file = event.target.files?.[0]
  if (!file || !document.value) return
  if (!canEdit.value) {
    ElMessage.warning('只读用户不能上传附件')
    event.target.value = ''
    return
  }
  const result = await uploadFile(file)
  const image = file.type.startsWith('image/')
  const snippet = image ? `![${result.name}](${result.url})` : `[${result.name}](${result.url})`
  if (activeEditorRef.value?.insertMarkdown) {
    activeEditorRef.value.insertMarkdown(snippet)
  } else {
    document.value.content = `${document.value.content || ''}\n\n${snippet}\n`
  }
  event.target.value = ''
}

function cleanError(message) {
  return message.replace(/\n/g, '').trim()
}

function canCreateIn(node) {
  return canEdit.value && (node.type === 'root' || node.type === 'folder')
}

function showKnowledgeOverview() {
  return !document.value && (!activeNode.value || activeNode.value.type === 'root')
}

async function openFolderChild(node) {
  activeNode.value = node
  if (node.type === 'document') {
    await openDocument(node)
  } else {
    document.value = null
  }
}

function roleLabel(role) {
  const labels = {
    admin: '管理员（admin）',
    editor: '编辑（editor）',
    viewer: '只读（viewer）'
  }
  return labels[role] || role
}
</script>

<template>
  <main v-if="setupChecking" class="login-shell">
    <section class="login-panel">
      <div>
        <h1>{{ appName }}</h1>
        <p>正在检查安装状态…</p>
      </div>
    </section>
  </main>

  <main v-else-if="setupNeeded" class="login-shell">
    <section class="login-panel setup-panel">
      <div>
        <h1>首次安装向导</h1>
        <p>创建超级管理员并完成基础配置后即可开始使用。</p>
      </div>
      <el-form label-position="top" @submit.prevent="submitSetup">
        <el-form-item label="项目名称">
          <el-input v-model="setupForm.app_name" maxlength="40" show-word-limit autocomplete="off" />
        </el-form-item>
        <el-form-item label="管理员账号">
          <el-input model-value="admin" disabled />
          <div class="form-tip">固定为 admin，作为系统超级管理员账号。</div>
        </el-form-item>
        <el-form-item label="管理员昵称">
          <el-input v-model="setupForm.admin_nickname" maxlength="40" autocomplete="off" />
        </el-form-item>
        <el-form-item label="管理员密码">
          <el-input
            v-model="setupForm.admin_password"
            type="password"
            show-password
            autocomplete="new-password"
            placeholder="至少 8 位，字母/数字/符号至少 2 种"
          />
        </el-form-item>
        <el-form-item label="确认密码">
          <el-input
            v-model="setupForm.admin_password_confirm"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="JWT 登录有效期（天）">
          <el-input-number
            v-model="setupForm.jwt_expire_days"
            :min="1"
            :max="90"
            :controls="false"
            class="full-width"
          />
        </el-form-item>
        <el-form-item label="新用户首次登录强制改密">
          <div class="setup-switch-row">
            <el-switch v-model="setupForm.force_password_change_new_users" />
            <span class="form-tip">可在安装后于项目配置中修改。</span>
          </div>
        </el-form-item>
        <el-alert v-if="setupError" :title="setupError" type="error" :closable="false" />
        <el-button type="primary" size="large" :loading="setupSaving" native-type="submit">
          {{ setupSaving ? '正在初始化' : '完成初始化并进入系统' }}
        </el-button>
      </el-form>
    </section>
  </main>

  <main v-else-if="!user" class="login-shell">
    <section v-if="!mfaChallenge" class="login-panel">
      <div>
        <h1>{{ appName }}</h1>
        <p>本地部署的团队 Markdown 文档库</p>
      </div>
      <el-form label-position="top" @submit.prevent="submitLogin">
        <el-form-item label="用户名">
          <el-input v-model="loginForm.username" autocomplete="username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="loginForm.password" type="password" autocomplete="current-password" show-password />
        </el-form-item>
        <el-alert v-if="loginError" :title="loginError" type="error" :closable="false" />
        <el-button type="primary" size="large" :loading="loading" native-type="submit">
          {{ loading ? '正在登录' : '登录文档系统' }}
        </el-button>
      </el-form>
    </section>

    <section v-else class="login-panel mfa-panel">
      <div>
        <h1>{{ mfaChallenge.must_bind_mfa ? '绑定 MFA' : '验证 MFA' }}</h1>
        <p>
          账号 {{ mfaChallenge.username }}
          {{ mfaChallenge.must_bind_mfa ? '必须绑定认证器 App 后才能进入系统。' : '已绑定 MFA。' }}
        </p>
      </div>
      <div v-if="mfaChallenge.must_bind_mfa" class="mfa-bind-box">
        <p>请使用 Google Authenticator、2FAS、Aegis 等 2FA 工具扫描下方二维码：</p>
        <div class="mfa-qr">
          <img :src="mfaChallenge.qr_data_url" alt="MFA 二维码" />
        </div>
        <div class="login-hint">无法扫码时可手动输入：{{ mfaChallenge.manual_key }}</div>
      </div>
      <p v-else class="mfa-note">请输入认证器 App 中显示的 6 位验证码完成验证。</p>
      <el-form label-position="top" @submit.prevent="submitLoginMFA">
        <el-form-item label="6 位验证码">
          <el-input
            ref="mfaCodeInput"
            v-model="mfaForm.code"
            maxlength="6"
            inputmode="numeric"
            autocomplete="one-time-code"
            @input="handleMFAInput"
          />
        </el-form-item>
        <div class="mfa-actions">
          <el-button @click="backToPasswordLogin">返回</el-button>
          <el-button type="primary" :loading="loading" native-type="submit">
            {{ mfaChallenge.must_bind_mfa ? '确认绑定' : '验证' }}
          </el-button>
        </div>
      </el-form>
    </section>
  </main>

  <main v-else-if="user.must_change_password" class="login-shell">
    <section class="login-panel">
      <div>
        <h1>修改密码</h1>
        <p>账号 {{ user.username }} 首次登录前必须修改初始密码。</p>
      </div>
      <el-form label-position="top" @submit.prevent="submitForcedPasswordChange">
        <el-form-item label="当前密码">
          <el-input v-model="myPasswordForm.current_password" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="myPasswordForm.new_password" type="password" show-password autocomplete="new-password" />
          <div class="form-tip">至少 8 位，字母 / 数字 / 特殊符号至少包含 2 种，且不能和当前密码一致</div>
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="myPasswordForm.confirm_password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <div class="mfa-actions">
          <el-button @click="signOut">退出登录</el-button>
          <el-button type="primary" :loading="myPasswordSaving" native-type="submit">更新密码并重新登录</el-button>
        </div>
      </el-form>
    </section>
  </main>

  <main v-else class="app-shell">
    <header class="topbar">
      <div class="brand">
        <span class="brand-name">{{ appName }}</span>
        <el-dropdown trigger="click" @command="setEditorEngine">
          <button class="editor-engine-switch" type="button">
            <span>{{ currentEditorEngineMeta.label }}</span>
            <el-icon><ArrowDown /></el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item
                v-for="item in editorEngineOptions"
                :key="item.id"
                :command="item.id"
                :disabled="editorEngine === item.id"
              >
                <div class="editor-engine-option">
                  <strong>{{ item.label }}</strong>
                  <span>{{ item.hint }}</span>
                </div>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
      <div class="top-actions">
        <el-button v-if="canEdit" :icon="'FolderAdd'" @click="createRootFolder">文件夹</el-button>
        <el-button v-if="canEdit" type="primary" :icon="'DocumentAdd'" @click="createDoc()">文档</el-button>
        <el-button v-if="canEdit" :icon="'Delete'" @click="openTrashDialog">回收站</el-button>
        <el-dropdown trigger="click">
          <button class="user-menu" type="button">
            <span class="user-avatar">{{ (user.nickname || user.username).slice(0, 1).toUpperCase() }}</span>
            <span>{{ user.nickname || user.username }}</span>
            <el-icon><ArrowDown /></el-icon>
          </button>
          <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>当前角色：{{ roleLabel(user.role) }}</el-dropdown-item>
                <el-dropdown-item :icon="'Lock'" @click="openMyPasswordDialog">修改密码</el-dropdown-item>
                <el-dropdown-item :icon="'Notebook'" @click="openOperationLogs">
                  操作日志
                </el-dropdown-item>
                <el-dropdown-item v-if="user.role === 'admin'" :icon="'User'" @click="openUserManager">
                  用户管理
                </el-dropdown-item>
                <el-dropdown-item v-if="isSuperAdmin" :icon="'Setting'" @click="openProjectConfig">
                  项目配置
                </el-dropdown-item>
                <el-dropdown-item divided :icon="'SwitchButton'" @click="signOut">退出登录</el-dropdown-item>
              </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </header>

    <section class="workspace">
      <aside class="sidebar">
        <div class="search-box sidebar-search">
          <el-input
            v-model="searchQuery"
            placeholder="搜索标题"
            clearable
            :prefix-icon="'Search'"
            @keyup.enter="runSearch"
            @clear="clearDocumentSearch"
          />
          <el-button :icon="'Search'" :loading="searchLoading" @click="runSearch" />
        </div>

        <div v-if="searchResults.length" class="search-results">
          <div class="side-title">搜索结果</div>
          <button
            v-for="item in searchResults"
            :key="item.id"
            class="result-item"
            @click="openDocument({ id: item.id })"
          >
            {{ item.title }}
          </button>
        </div>
        <div v-else-if="searchCompleted" class="search-empty">未找到匹配标题</div>

        <el-tree
          :data="tree"
          node-key="key"
          :props="{ label: 'title', children: 'children' }"
          default-expand-all
          highlight-current
          :draggable="canEdit"
          :allow-drop="allowTreeDrop"
          @node-click="handleNodeClick"
          @node-drop="handleTreeDrop"
        >
          <template #default="{ data }">
            <div class="tree-row">
              <span class="tree-label">
                <el-icon v-if="data.type === 'root'"><Files /></el-icon>
                <el-icon v-else-if="data.type === 'folder'"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span>{{ data.title }}</span>
              </span>
              <span class="tree-actions">
                <el-tooltip v-if="canCreateIn(data)" content="新建文档" placement="top" :show-after="80" :hide-after="0">
                  <button aria-label="新建文档" @click.stop="createDoc(data)">
                    <el-icon><DocumentAdd /></el-icon>
                  </button>
                </el-tooltip>
                <el-tooltip v-if="canCreateIn(data)" content="新建文件夹" placement="top" :show-after="80" :hide-after="0">
                  <button aria-label="新建文件夹" @click.stop="createChildFolder(data)">
                    <el-icon><FolderAdd /></el-icon>
                  </button>
                </el-tooltip>
                <el-tooltip v-if="canCreateIn(data)" content="导入文件" placement="top" :show-after="80" :hide-after="0">
                  <button aria-label="导入文件" @click.stop="importFileToFolder(data)">
                    <el-icon><Upload /></el-icon>
                  </button>
                </el-tooltip>
                <el-tooltip v-if="canEdit && data.type !== 'root'" content="重命名" placement="top" :show-after="80" :hide-after="0">
                  <button aria-label="重命名" @click.stop="renameNode(data)">
                    <el-icon><Edit /></el-icon>
                  </button>
                </el-tooltip>
                <el-tooltip v-if="canEdit && data.type !== 'root'" content="删除" placement="top" :show-after="80" :hide-after="0">
                  <button aria-label="删除" @click.stop="removeNode(data)">
                    <el-icon><Delete /></el-icon>
                  </button>
                </el-tooltip>
              </span>
            </div>
          </template>
        </el-tree>
      </aside>

      <section class="editor-area">
        <template v-if="document">
          <div class="doc-header">
            <el-input v-if="canEdit" v-model="document.title" class="title-input" />
            <h1 v-else class="readonly-title">{{ document.title }}</h1>
            <div class="doc-actions">
              <el-segmented v-if="canEdit" v-model="editorMode" :options="editorModes" />
              <el-segmented
                v-if="effectiveEditorMode === 'preview'"
                v-model="previewWidth"
                :options="previewWidths"
                class="preview-width-control"
              />
              <input ref="fileInput" type="file" class="hidden-input" @change="insertUpload" />
              <el-tag v-if="!canEdit" type="info">只读</el-tag>
              <el-button
                :type="outlineVisible ? 'primary' : 'default'"
                plain
                :icon="'Menu'"
                @click="toggleOutline"
              >
                大纲
              </el-button>
              <el-button :icon="'Clock'" @click="openHistoryDialog">历史</el-button>
              <el-button v-if="canEdit" :icon="'Upload'" @click="fileInput.click()">上传</el-button>
              <el-button v-if="canEdit" type="primary" :loading="saving" :icon="'Check'" @click="saveCurrent">保存</el-button>
            </div>
          </div>

          <div class="editor-body">
            <div class="editor-grid" :class="`mode-${effectiveEditorMode}`">
              <ByteMdEditor
                v-if="editorEngine === 'bytemd'"
                ref="activeEditorRef"
                v-model="document.content"
                :mode="effectiveEditorMode"
                :preview-width="previewWidth"
                :readonly="!canEdit"
                @save="saveCurrent"
              />
              <VditorEditor
                v-else
                ref="activeEditorRef"
                v-model="document.content"
                :mode="effectiveEditorMode"
                :preview-width="previewWidth"
                :readonly="!canEdit"
                @save="saveCurrent"
              />
            </div>
            <DocOutline
              v-if="outlineVisible"
              :items="outlineItems"
              :active-id="activeOutlineId"
              @select="handleOutlineSelect"
            />
          </div>
        </template>

        <div v-else-if="showKnowledgeOverview()" class="overview-page">
          <section class="overview-hero">
            <div>
              <p class="overview-kicker">知识库说明</p>
              <h1>公司 IT 团队本地文档中心</h1>
              <p>
                这里用于沉淀项目架构、技术方案、部署说明、接口文档和工作复盘。文档正文以 Markdown
                文件保存在本地，系统只用 SQLite 管理目录、用户、排序和附件等元数据。
              </p>
            </div>
            <div v-if="canEdit" class="overview-actions">
              <el-button type="primary" :icon="'DocumentAdd'" @click="createDoc(activeNode)">新建文档</el-button>
              <el-button :icon="'FolderAdd'" @click="createChildFolder(activeNode)">新建文件夹</el-button>
            </div>
          </section>

          <section class="overview-section">
            <h2>适合记录什么</h2>
            <div class="overview-grid">
              <div>
                <h3>项目架构</h3>
                <p>系统边界、模块关系、技术栈、核心链路和关键依赖。</p>
              </div>
              <div>
                <h3>技术文档</h3>
                <p>接口说明、部署流程、运维手册、开发规范和排障经验。</p>
              </div>
              <div>
                <h3>复盘沉淀</h3>
                <p>需求复盘、事故复盘、性能优化记录和团队协作经验。</p>
              </div>
            </div>
          </section>

          <section class="overview-section">
            <h2>当前项目架构</h2>
            <div class="architecture-map">
              <div>浏览器 / Vue 3</div>
              <span>HTTP</span>
              <div>Go API 服务</div>
              <span>读写</span>
              <div>SQLite + Markdown + uploads</div>
            </div>
            <ul class="overview-list">
              <li>SQLite 存用户、文件夹、文档标题、路径、排序和附件记录。</li>
              <li>Markdown 正文存放在本地 data/docs/，不写入数据库。</li>
              <li>图片和附件存放在本地 data/uploads/。</li>
              <li>后端可以直接托管前端构建产物，部署时只需要一个 Go 服务。</li>
            </ul>
          </section>

          <section class="overview-section">
            <h2>建议目录</h2>
            <pre class="overview-tree">知识库
├── 项目架构
├── 技术规范
├── 部署运维
├── 接口文档
└── 复盘总结</pre>
          </section>
        </div>

        <div v-else-if="showFolderDashboard" class="folder-dashboard">
          <section class="folder-dashboard-hero">
            <p class="folder-dashboard-kicker">文件夹</p>
            <h1>{{ activeNode.title }}</h1>
            <p>
              在这里管理当前文件夹：新建文档、创建子文件夹、导入文件，或打开下方已有内容。
            </p>
          </section>

          <section v-if="canEdit" class="folder-quick-actions">
            <button type="button" class="folder-action-card" @click="createDoc(activeNode)">
              <el-icon><DocumentAdd /></el-icon>
              <span>
                <strong>新建文档</strong>
                <em>在此文件夹创建 Markdown 文档</em>
              </span>
            </button>
            <button type="button" class="folder-action-card" @click="createChildFolder(activeNode)">
              <el-icon><FolderAdd /></el-icon>
              <span>
                <strong>新建子文件夹</strong>
                <em>继续细分目录结构</em>
              </span>
            </button>
            <button type="button" class="folder-action-card" @click="importFileToFolder(activeNode)">
              <el-icon><Upload /></el-icon>
              <span>
                <strong>导入文件</strong>
                <em>支持 Markdown、Word、PDF、Excel 等</em>
              </span>
            </button>
            <button type="button" class="folder-action-card" @click="renameNode(activeNode)">
              <el-icon><Edit /></el-icon>
              <span>
                <strong>重命名</strong>
                <em>修改当前文件夹名称</em>
              </span>
            </button>
          </section>

          <section v-else class="folder-readonly-tip">
            当前为只读账号，可以浏览下方文档，但不能新建或修改内容。
          </section>

          <section class="folder-contents">
            <div class="folder-contents-header">
              <h2>本文件夹内容</h2>
              <span>{{ folderChildren.length }} 项</span>
            </div>

            <div v-if="folderChildren.length" class="folder-contents-list">
              <button
                v-for="item in folderSubfolders"
                :key="`folder-${item.id}`"
                type="button"
                class="folder-content-row"
                @click="openFolderChild(item)"
              >
                <el-icon><Folder /></el-icon>
                <strong>{{ item.title }}</strong>
                <em>文件夹</em>
              </button>
              <button
                v-for="item in folderDocuments"
                :key="`document-${item.id}`"
                type="button"
                class="folder-content-row"
                @click="openFolderChild(item)"
              >
                <el-icon><Document /></el-icon>
                <strong>{{ item.title }}</strong>
                <em>文档</em>
              </button>
            </div>
            <el-empty v-else description="这个文件夹还是空的，可以从上方快捷操作开始" />
          </section>

          <section v-if="canEdit" class="folder-danger-zone">
            <button type="button" class="folder-danger-btn" @click="removeNode(activeNode)">
              <el-icon><Delete /></el-icon>
              将此文件夹移入回收站
            </button>
          </section>
        </div>

        <div v-else class="empty-state">
          <el-icon><DocumentAdd /></el-icon>
          <span>选择或新建一篇文档</span>
        </div>
      </section>

      <el-dialog v-model="historyDialogVisible" title="文档历史版本" width="920px" class="history-dialog">
        <div class="history-toolbar">
          <span>每次保存若标题或正文有变化，会先保留当前内容为历史版本（每篇最多 50 个）。</span>
          <el-button :icon="'Refresh'" @click="loadHistoryItems">刷新</el-button>
        </div>
        <div v-loading="historyLoading" class="history-layout">
          <div class="history-list">
            <el-table
              v-if="historyItems.length"
              :data="historyItems"
              highlight-current-row
              :row-class-name="({ row }) => (row.id === selectedHistoryId ? 'is-current' : '')"
              @row-click="selectHistoryVersion"
            >
              <el-table-column label="版本" width="78">
                <template #default="{ row }">v{{ row.version_no }}</template>
              </el-table-column>
              <el-table-column label="标题" min-width="140" prop="title" show-overflow-tooltip />
              <el-table-column label="保存人" width="100" prop="created_by_name" show-overflow-tooltip />
              <el-table-column label="时间" min-width="150">
                <template #default="{ row }">{{ formatBeijingTime(row.created_at) }}</template>
              </el-table-column>
            </el-table>
            <el-empty v-else description="暂无历史版本，保存修改后会出现在这里" />
          </div>
          <div v-loading="historyPreviewLoading" class="history-preview">
            <template v-if="historyPreview">
              <div class="history-preview-meta">
                <strong>v{{ historyPreview.version_no }} · {{ historyPreview.title }}</strong>
                <span>{{ formatBeijingTime(historyPreview.created_at) }} · {{ historyPreview.created_by_name }}</span>
              </div>
              <pre class="history-preview-content">{{ historyPreview.content }}</pre>
            </template>
            <el-empty v-else description="选择左侧版本查看内容" />
          </div>
        </div>
        <template #footer>
          <el-button @click="historyDialogVisible = false">关闭</el-button>
          <el-button
            v-if="canEdit"
            type="primary"
            :disabled="!selectedHistoryId"
            :loading="historyRestoring"
            @click="restoreHistoryVersion"
          >
            恢复此版本
          </el-button>
        </template>
      </el-dialog>

      <el-dialog v-model="trashDialogVisible" title="回收站" width="760px">
        <div class="trash-toolbar">
          <span>删除的文档和文件夹会先进入回收站，永久删除后不可恢复。</span>
          <el-button :icon="'Refresh'" @click="loadTrashItems">刷新</el-button>
        </div>
        <el-table v-if="trashItems.length" v-loading="trashLoading" :data="trashItems" class="trash-table">
          <el-table-column label="名称" min-width="220">
            <template #default="{ row }">
              <div class="trash-item-name">
                <el-icon v-if="row.type === 'folder'"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <div>
                  <strong>{{ row.title }}</strong>
                  <span>{{ row.type === 'folder' ? '文件夹' : '文档' }}</span>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="影响范围" min-width="120">
            <template #default="{ row }">{{ trashImpact(row) }}</template>
          </el-table-column>
          <el-table-column label="删除时间" min-width="165">
            <template #default="{ row }">{{ formatBeijingTime(row.deleted_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="145" fixed="right">
            <template #default="{ row }">
              <div class="table-actions">
                <el-button link type="primary" @click="restoreTrash(row)">恢复</el-button>
                <el-button link type="danger" @click="purgeTrash(row)">永久删除</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-else v-loading="trashLoading" class="trash-empty">
          <el-empty description="回收站为空" />
        </div>
        <div v-if="trashTotal > trashPageSize" class="trash-pagination">
          <el-pagination
            v-model:current-page="trashPage"
            v-model:page-size="trashPageSize"
            :total="trashTotal"
            :page-sizes="[10, 20, 50]"
            layout="total, sizes, prev, pager, next"
            background
            @current-change="handleTrashPageChange"
            @size-change="handleTrashPageSizeChange"
          />
        </div>
      </el-dialog>

      <el-dialog v-model="logsDialogVisible" title="操作日志" width="980px" class="logs-dialog">
        <div class="logs-toolbar">
          <div class="logs-hint">
            {{ logsScopeAll ? '管理员可查看全部用户的操作记录。' : '当前仅显示你自己的操作记录。' }}
          </div>
          <div class="logs-filters">
            <el-select
              v-model="logsActionFilter"
              clearable
              placeholder="全部操作类型"
              style="width: 200px"
              @change="searchOperationLogs"
            >
              <el-option
                v-for="item in logActions"
                :key="item.value"
                :label="item.label"
                :value="item.value"
              />
            </el-select>
            <el-input
              v-model="logsKeyword"
              :placeholder="logsScopeAll ? '搜索用户或详情' : '搜索详情'"
              clearable
              :prefix-icon="'Search'"
              style="width: 240px"
              @keyup.enter="searchOperationLogs"
              @clear="searchOperationLogs"
            />
            <el-button type="primary" :icon="'Search'" @click="searchOperationLogs">搜索</el-button>
            <el-button :icon="'Refresh'" @click="clearOperationLogSearch">重置</el-button>
          </div>
        </div>
        <el-table v-if="logItems.length || logsLoading" v-loading="logsLoading" :data="logItems" class="logs-table">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ formatBeijingTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column v-if="logsScopeAll" label="操作人" min-width="150">
            <template #default="{ row }">{{ logOperatorLabel(row) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="140">
            <template #default="{ row }">
              <el-tag size="small" type="info">{{ row.action_label || row.action }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="详情" min-width="280" prop="detail" show-overflow-tooltip />
        </el-table>
        <div v-else class="logs-empty">
          <el-empty description="暂无操作日志" />
        </div>
        <div v-if="logsTotal > 0" class="logs-pagination">
          <el-pagination
            v-model:current-page="logsPage"
            v-model:page-size="logsPageSize"
            :total="logsTotal"
            :page-sizes="[10, 20, 50]"
            layout="total, sizes, prev, pager, next"
            background
            @current-change="handleLogsPageChange"
            @size-change="handleLogsPageSizeChange"
          />
        </div>
      </el-dialog>

      <el-dialog v-model="userDialogVisible" title="用户管理" width="920px">
        <div class="user-manager-toolbar">
          <div class="user-manager-actions">
            <el-button type="primary" :icon="'UserFilled'" @click="openCreateUser">新建用户</el-button>
            <el-button :icon="'Refresh'" @click="loadUsers">刷新</el-button>
          </div>
          <div class="user-search">
            <el-input
              v-model="userSearchUsername"
              placeholder="按用户名/手机号搜索"
              clearable
              :prefix-icon="'Search'"
              @keyup.enter="searchUsers"
              @clear="clearUserSearch"
            />
            <el-button type="primary" :icon="'Search'" @click="searchUsers">搜索</el-button>
          </div>
        </div>
        <el-table v-loading="usersLoading" :data="users" class="user-table">
          <el-table-column label="用户" min-width="190">
            <template #default="{ row }">
              <div class="user-identity">
                <strong>{{ row.username }}</strong>
                <span>{{ row.nickname || '未设置昵称' }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="role" label="角色" width="140">
            <template #default="{ row }">
              <el-tag
                size="small"
                class="role-tag"
                :type="row.role === 'admin' ? 'danger' : row.role === 'editor' ? 'primary' : 'info'"
              >
                {{ roleLabel(row.role) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column width="140">
            <template #header>
              <span class="security-header">
                MFA 状态
                <el-tooltip
                  effect="dark"
                  placement="top"
                  content="MFA 开启后，用户登录时需要输入认证器 App 的 6 位验证码；未绑定时会先扫码绑定。"
                >
                  <span class="mfa-help">?</span>
                </el-tooltip>
              </span>
            </template>
            <template #default="{ row }">
              <div class="security-cell">
                <el-tag v-if="row.must_change_password" size="small" type="warning">待改密</el-tag>
                <div class="security-row">
                  <el-switch
                    v-model="row.mfa_enabled"
                    size="small"
                    @change="(value) => toggleUserMFA(row, value)"
                  />
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="285">
            <template #default="{ row }">
              <div class="table-actions">
                <el-button link type="primary" @click="openEditUser(row)">编辑</el-button>
                <el-button link type="primary" :disabled="row.username === 'admin'" @click="openResetPassword(row)">
                  重置密码
                </el-button>
                <el-button link type="primary" :disabled="!row.mfa_enabled" @click="resetMFA(row)">重置 MFA</el-button>
                <el-button link type="danger" :disabled="row.id === user.id" @click="removeUser(row)">删除</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="usersTotal > userPageSize" class="user-pagination">
          <el-pagination
            v-model:current-page="userPage"
            v-model:page-size="userPageSize"
            :total="usersTotal"
            :page-sizes="[10, 20, 50]"
            layout="total, sizes, prev, pager, next"
            background
            @current-change="handleUserPageChange"
            @size-change="handleUserPageSizeChange"
          />
        </div>
      </el-dialog>

      <el-dialog v-model="projectConfigDialogVisible" title="项目配置" width="520px">
        <div class="project-config-panel">
          <div class="config-line">
            <div>
              <strong>项目名称</strong>
              <span class="config-help">显示在登录页标题和系统左上角，默认值为 Doc System。</span>
            </div>
            <div class="config-input-group">
              <el-input
                v-model="settings.app_name"
                maxlength="40"
                show-word-limit
                autocomplete="off"
                @keyup.enter="saveAppNameSetting"
              />
              <el-button type="primary" :loading="settingsSaving" @click="saveAppNameSetting">保存</el-button>
            </div>
          </div>
          <div class="config-line">
            <div>
              <strong>新用户首次登录强制改密</strong>
              <span class="config-help">开启后，管理员新建的用户首次登录必须先修改密码。默认关闭。</span>
            </div>
            <el-switch
              v-model="settings.force_password_change_new_users"
              :loading="settingsSaving"
              @change="saveForcePasswordSetting"
            />
          </div>
          <div class="config-line">
            <div>
              <strong>MFA 失败限制</strong>
              <span class="config-help">开启 MFA 的用户在指定时间内连续输错达到上限后，需要重新输入账号密码。默认 120 秒内 5 次。</span>
            </div>
            <div class="config-number-group">
              <el-input-number
                v-model="settings.mfa_failed_window_seconds"
                size="small"
                :min="30"
                :max="3600"
                :step="30"
                :controls="false"
                @change="saveMFAFailureSettings"
              />
              <span>秒内</span>
              <el-input-number
                v-model="settings.mfa_failed_max_attempts"
                size="small"
                :min="1"
                :max="20"
                :controls="false"
                @change="saveMFAFailureSettings"
              />
              <span>次</span>
            </div>
          </div>
          <div class="config-line">
            <div>
              <strong>JWT 登录有效期</strong>
              <span class="config-help">登录后 Cookie / JWT 的有效天数，范围 1～90 天，默认 1 天。修改后仅对新登录生效。</span>
            </div>
            <div class="config-number-group">
              <el-input-number
                v-model="settings.jwt_expire_days"
                size="small"
                :min="1"
                :max="90"
                :controls="false"
                @change="saveJWTExpireDays"
              />
              <span>天</span>
            </div>
          </div>
        </div>
      </el-dialog>

      <el-dialog v-model="userFormVisible" :title="editingUser ? '编辑用户' : '新建用户'" width="420px">
        <el-form label-position="top">
          <el-form-item label="用户名">
            <el-input v-model="userForm.username" :disabled="!!editingUser" autocomplete="off" />
          </el-form-item>
          <el-form-item v-if="!editingUser" label="初始密码">
            <el-input v-model="userForm.password" type="password" show-password autocomplete="new-password" />
          </el-form-item>
          <el-form-item label="昵称">
            <el-input v-model="userForm.nickname" autocomplete="off" />
          </el-form-item>
          <el-form-item label="角色">
            <el-select v-model="userForm.role" class="full-width" :disabled="editingUser?.username === 'admin'">
              <el-option label="管理员（admin）" value="admin" />
              <el-option label="编辑（editor）" value="editor" />
              <el-option label="只读（viewer）" value="viewer" />
            </el-select>
            <div v-if="editingUser?.username === 'admin'" class="form-tip">初始化 admin 用户固定为管理员角色</div>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="userFormVisible = false">取消</el-button>
          <el-button type="primary" :loading="userSaving" @click="submitUserForm">保存</el-button>
        </template>
      </el-dialog>

      <el-dialog v-model="passwordDialogVisible" title="重置密码" width="420px">
        <el-form label-position="top">
          <el-form-item label="用户">
            <el-input :model-value="passwordUser?.username || ''" disabled />
          </el-form-item>
          <el-form-item label="新密码">
            <el-input v-model="passwordForm.password" type="password" show-password autocomplete="new-password">
              <template #append>
                <el-button @click="createPassword">创建密码</el-button>
              </template>
            </el-input>
            <div class="form-tip">自动创建的密码满足至少 8 位，并包含字母、数字和特殊符号。</div>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button :loading="passwordSaving" @click="copyPasswordAndReset">复制密码并保存</el-button>
          <el-button @click="passwordDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="passwordSaving" @click="submitPasswordReset">保存</el-button>
        </template>
      </el-dialog>

      <el-dialog v-model="myPasswordDialogVisible" title="修改密码" width="420px">
        <el-form label-position="top">
          <el-form-item label="当前密码">
            <el-input
              v-model="myPasswordForm.current_password"
              type="password"
              show-password
              autocomplete="current-password"
            />
          </el-form-item>
          <el-form-item label="新密码">
            <el-input
              v-model="myPasswordForm.new_password"
              type="password"
              show-password
              autocomplete="new-password"
            />
            <div class="form-tip">至少 8 位，字母 / 数字 / 特殊符号至少包含 2 种，且不能和当前密码一致</div>
          </el-form-item>
          <el-form-item label="确认新密码">
            <el-input
              v-model="myPasswordForm.confirm_password"
              type="password"
              show-password
              autocomplete="new-password"
            />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="myPasswordDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="myPasswordSaving" @click="submitMyPasswordChange">保存</el-button>
        </template>
      </el-dialog>
    </section>
  </main>
</template>
