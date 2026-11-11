// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import {
  App as AntdApp,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SafetyOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { ColumnsType } from 'antd/es/table'
import {
  AdminUser,
  CreateUserPayload,
  ProductCatalogEntry,
  ProductRoleAssignments,
  ResetPasswordPayload,
  UpdateUserPayload,
  createAdminUser,
  deleteAdminUser,
  getProductRoles,
  listAdminUsers,
  productCatalog,
  resetUserPassword,
  setProductRoles,
  setUserEnabled,
  setUserRoles,
  updateAdminUser,
} from '../api/admin'
import { extractError } from '../api/client'
import ProductRolesEditor from '../components/ProductRolesEditor'
import { useAuth } from '../hooks/useAuth'
import { useHelpStore } from '../help/helpStore'

const { Title, Paragraph } = Typography
const SUITE_ROLES = ['user', 'operator', 'admin']

interface EditFormValues extends UpdateUserPayload {
  product_roles?: ProductRoleAssignments
}

export default function AdminUsersPage() {
  const { t } = useTranslation()
  const { identity, loading } = useAuth()
  const { message, modal } = AntdApp.useApp()
  const openHelp = useHelpStore((state) => state.open)

  const [users, setUsers] = useState<AdminUser[]>([])
  const [total, setTotal] = useState(0)
  const [loadingUsers, setLoadingUsers] = useState(false)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AdminUser | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AdminUser | null>(null)
  const [deleteConfirmation, setDeleteConfirmation] = useState('')
  const [resetTarget, setResetTarget] = useState<AdminUser | null>(null)
  const [form] = Form.useForm<CreateUserPayload>()
  const [editForm] = Form.useForm<EditFormValues>()
  const [resetForm] = Form.useForm<ResetPasswordPayload>()
  const [catalog, setCatalog] = useState<ProductCatalogEntry[]>([])

  const isAdmin = identity?.roles.includes('admin') ?? false
  const canRead = isAdmin || (identity?.roles.includes('operator') ?? false)
  const currentUserId = identity?.sub ?? ''

  const showError = useCallback(
    (err: unknown) => {
      const parsed = extractError(err)
      const key = `admin.users.errors.${parsed.code}`
      const translated = t(key)
      message.error(translated === key ? parsed.message : translated)
    },
    [message, t],
  )

  const refresh = useCallback(async () => {
    setLoadingUsers(true)
    try {
      const result = await listAdminUsers(search, (page - 1) * pageSize, pageSize)
      setUsers(result.users)
      setTotal(result.total)
    } catch (err) {
      showError(err)
    } finally {
      setLoadingUsers(false)
    }
  }, [page, pageSize, search, showError])

  useEffect(() => {
    if (canRead) void refresh()
  }, [canRead, refresh])

  useEffect(() => {
    if (!canRead) return
    let cancelled = false
    void productCatalog()
      .then((products) => {
        if (!cancelled) setCatalog(products)
      })
      .catch(() => {
        // Product-role assignment is unavailable (e.g. local emergency mode).
        if (!cancelled) setCatalog([])
      })
    return () => {
      cancelled = true
    }
  }, [canRead])

  if (!loading && !identity) return <Navigate to="/login?redirect=%2Fadmin%2Fusers" replace />
  if (!loading && !canRead) return <Navigate to="/" replace />

  const roleLabel = (role: string) => {
    const label = t(`admin.users.roleNames.${role}`)
    return label === `admin.users.roleNames.${role}` ? role : label
  }

  const actionLabel = (action: string) => {
    const label = t(`admin.users.requiredActions.${action}`)
    return label === `admin.users.requiredActions.${action}` ? action : label
  }

  const onCreate = async () => {
    const values = await form.validateFields()
    const payload: CreateUserPayload = {
      ...values,
      roles: values.roles?.length ? values.roles : ['user'],
    }
    if (!payload.product_roles || Object.keys(payload.product_roles).length === 0) {
      delete payload.product_roles
    }
    try {
      await createAdminUser(payload)
      message.success(t('admin.users.created'))
      setCreateOpen(false)
      form.resetFields()
      await refresh()
    } catch (err) {
      showError(err)
    }
  }

  const openEdit = (user: AdminUser) => {
    setEditTarget(user)
    editForm.setFieldsValue({
      email: user.email ?? '',
      first_name: user.first_name ?? '',
      last_name: user.last_name ?? '',
      product_roles: {},
    })
    if (catalog.length > 0) {
      void getProductRoles(user.id)
        .then((assignments) => editForm.setFieldsValue({ product_roles: assignments }))
        .catch(() => {
          // Keep the profile fields editable even if assignments cannot load.
        })
    }
  }

  const onEdit = async () => {
    const values = await editForm.validateFields()
    if (!editTarget) return
    const { product_roles, ...profile } = values
    try {
      await updateAdminUser(editTarget.id, profile)
      if (catalog.length > 0) {
        await setProductRoles(editTarget.id, product_roles ?? {})
      }
      message.success(t('admin.users.updated'))
      setEditTarget(null)
      await refresh()
    } catch (err) {
      showError(err)
    }
  }

  const onToggleEnabled = async (user: AdminUser, enabled: boolean) => {
    try {
      await setUserEnabled(user.id, enabled)
      message.success(t('admin.users.updated'))
      await refresh()
    } catch (err) {
      showError(err)
      await refresh()
    }
  }

  const onResetPassword = async () => {
    const values = await resetForm.validateFields()
    if (!resetTarget) return
    try {
      await resetUserPassword(resetTarget.id, values)
      message.success(t('admin.users.passwordReset'))
      setResetTarget(null)
      resetForm.resetFields()
    } catch (err) {
      showError(err)
    }
  }

  const onDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteAdminUser(deleteTarget.id)
      message.success(t('admin.users.deleted'))
      setDeleteTarget(null)
      setDeleteConfirmation('')
      await refresh()
    } catch (err) {
      showError(err)
    }
  }

  const onChangeRoles = (user: AdminUser, roles: string[]) => {
    modal.confirm({
      title: t('admin.users.confirmRoleTitle'),
      content: t('admin.users.confirmRoleContent', { name: user.username, roles: roles.map(roleLabel).join(', ') }),
      okText: t('admin.users.confirmChange'),
      cancelText: t('admin.users.cancel'),
      onOk: async () => {
        try {
          await setUserRoles(user.id, roles)
          message.success(t('admin.users.updated'))
          await refresh()
        } catch (err) {
          showError(err)
          await refresh()
        }
      },
      onCancel: () => void refresh(),
    })
  }

  const columns: ColumnsType<AdminUser> = [
    {
      title: t('admin.users.columns.username'),
      dataIndex: 'username',
      render: (value: string, user) => (
        <Space direction="vertical" size={0}>
          <Space>
            {value}
            {user.federated && <Tag color="purple">{user.federation_provider ?? t('admin.users.external')}</Tag>}
            {user.required_actions.map((action) => (
              <Tag key={action} color="orange">
                {actionLabel(action)}
              </Tag>
            ))}
          </Space>
        </Space>
      ),
    },
    { title: t('admin.users.columns.name'), render: (_, u) => [u.first_name, u.last_name].filter(Boolean).join(' ') },
    {
      title: t('admin.users.columns.email'),
      render: (_, user) => (
        <Space direction="vertical" size={0}>
          <span>{user.email || '-'}</span>
          {user.email && (
            <Tag color={user.email_verified ? 'green' : 'default'}>
              {user.email_verified ? t('admin.users.emailVerified') : t('admin.users.emailUnverified')}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('admin.users.columns.roles'),
      dataIndex: 'roles',
      render: (roles: string[], user) =>
        user.federated || !isAdmin || user.id === currentUserId ? (
          <Space wrap>
            {roles.map((role) => (
              <Tag key={role}>{roleLabel(role)}</Tag>
            ))}
          </Space>
        ) : (
          <Select
            mode="multiple"
            size="small"
            style={{ minWidth: 210 }}
            value={roles}
            options={SUITE_ROLES.map((role) => ({ label: roleLabel(role), value: role }))}
            onChange={(next) => onChangeRoles(user, next)}
          />
        ),
    },
    {
      title: t('admin.users.columns.enabled'),
      dataIndex: 'enabled',
      width: 110,
      render: (enabled: boolean, user) =>
        user.federated || !isAdmin ? (
          <Tag color={enabled ? 'green' : 'default'}>
            {enabled ? t('admin.users.enabledTrue') : t('admin.users.enabledFalse')}
          </Tag>
        ) : (
          <Popconfirm
            title={enabled ? t('admin.users.confirmDisableTitle') : t('admin.users.confirmEnableTitle')}
            description={user.username}
            okText={t('admin.users.confirmChange')}
            cancelText={t('admin.users.cancel')}
            onConfirm={() => onToggleEnabled(user, !enabled)}
          >
            <Switch checked={enabled} disabled={user.id === currentUserId} />
          </Popconfirm>
        ),
    },
    {
      title: t('admin.users.columns.actions'),
      width: 270,
      render: (_, user) =>
        user.federated || !isAdmin ? (
          <Tooltip title={t('admin.users.federatedReadOnly')}>
            <Tag>{t('admin.users.managedExternally')}</Tag>
          </Tooltip>
        ) : (
          <Space size={4}>
            <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(user)}>
              {t('admin.users.edit')}
            </Button>
            <Button size="small" onClick={() => setResetTarget(user)}>
              {t('admin.users.resetPassword')}
            </Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined />}
              disabled={user.id === currentUserId}
              onClick={() => {
                setDeleteTarget(user)
                setDeleteConfirmation('')
              }}
            >
              {t('admin.users.delete')}
            </Button>
          </Space>
        ),
    },
  ]

  return (
    <div style={{ maxWidth: 1280, margin: '0 auto' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%', marginBottom: 16 }}>
        <div>
          <Title level={3} style={{ marginBottom: 4 }}>
            <SafetyOutlined /> {t('admin.users.title')}
          </Title>
          <Paragraph type="secondary" style={{ marginBottom: 0 }}>
            {t('admin.users.subtitle')}
          </Paragraph>
        </div>
        <Space>
          <Tooltip title={t('common.help')}>
            <Button icon={<QuestionCircleOutlined />} onClick={() => openHelp('admin-users')} />
          </Tooltip>
          <Tooltip title={t('admin.users.refresh')}>
            <Button icon={<ReloadOutlined />} onClick={refresh} loading={loadingUsers} />
          </Tooltip>
          {isAdmin && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              {t('admin.users.create')}
            </Button>
          )}
        </Space>
      </Space>

      <Card>
        <Space style={{ marginBottom: 16 }}>
          <Input.Search
            allowClear
            style={{ width: 320 }}
            placeholder={t('admin.users.searchPlaceholder')}
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            onSearch={(value) => {
              setPage(1)
              setSearch(value.trim())
            }}
          />
        </Space>
        <Table
          rowKey="id"
          loading={loadingUsers}
          columns={columns}
          dataSource={users}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (value) => t('admin.users.total', { total: value }),
            onChange: (nextPage, nextSize) => {
              setPage(nextSize === pageSize ? nextPage : 1)
              setPageSize(nextSize)
            },
          }}
        />
      </Card>

      <Modal
        title={t('admin.users.createTitle')}
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={onCreate}
        okText={t('admin.users.confirmCreate')}
        cancelText={t('admin.users.cancel')}
        width={catalog.length > 0 ? 720 : 520}
        destroyOnClose
      >
        <Form
          form={form}
          name="create-suite-account"
          layout="vertical"
          style={{ marginTop: 16 }}
          initialValues={{ roles: ['user'], temporary: true }}
        >
          <Form.Item
            name="username"
            label={t('admin.users.fields.username')}
            rules={[{ required: true, message: t('admin.users.fields.usernameRequired') }]}
          >
            <Input autoFocus />
          </Form.Item>
          <Form.Item name="email" label={t('admin.users.fields.email')}>
            <Input type="email" />
          </Form.Item>
          <Space>
            <Form.Item name="first_name" label={t('admin.users.fields.firstName')}>
              <Input />
            </Form.Item>
            <Form.Item name="last_name" label={t('admin.users.fields.lastName')}>
              <Input />
            </Form.Item>
          </Space>
          <Form.Item
            name="password"
            label={t('admin.users.fields.password')}
            rules={[
              { required: true, message: t('admin.users.fields.passwordRequired') },
              { min: 8, message: t('admin.users.fields.passwordTooShort') },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="temporary" valuePropName="checked">
            <Checkbox>{t('admin.users.fields.temporaryPassword')}</Checkbox>
          </Form.Item>
          <Form.Item name="roles" label={t('admin.users.fields.roles')}>
            <Select mode="multiple" options={SUITE_ROLES.map((role) => ({ label: roleLabel(role), value: role }))} />
          </Form.Item>
          {catalog.length > 0 && (
            <Form.Item
              name="product_roles"
              label={t('admin.users.productRoles.title')}
              // This field contains several inputs rather than one control;
              // ProductRolesEditor labels each input individually.
              htmlFor={undefined}
              extra={t('admin.users.productRoles.subtitle')}
            >
              <ProductRolesEditor catalog={catalog} />
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title={t('admin.users.editTitle', { name: editTarget?.username ?? '' })}
        open={editTarget !== null}
        onCancel={() => setEditTarget(null)}
        onOk={onEdit}
        okText={t('admin.users.confirmChange')}
        cancelText={t('admin.users.cancel')}
        width={catalog.length > 0 ? 720 : 520}
        destroyOnClose
      >
        <Form form={editForm} name="edit-suite-account" layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item name="email" label={t('admin.users.fields.email')}>
            <Input type="email" />
          </Form.Item>
          <Space>
            <Form.Item name="first_name" label={t('admin.users.fields.firstName')}>
              <Input />
            </Form.Item>
            <Form.Item name="last_name" label={t('admin.users.fields.lastName')}>
              <Input />
            </Form.Item>
          </Space>
          {catalog.length > 0 && (
            <Form.Item
              name="product_roles"
              label={t('admin.users.productRoles.title')}
              // This field contains several inputs rather than one control;
              // ProductRolesEditor labels each input individually.
              htmlFor={undefined}
              extra={t('admin.users.productRoles.subtitle')}
            >
              <ProductRolesEditor catalog={catalog} />
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title={t('admin.users.resetTitle', { name: resetTarget?.username ?? '' })}
        open={resetTarget !== null}
        onCancel={() => setResetTarget(null)}
        onOk={onResetPassword}
        okText={t('admin.users.confirmReset')}
        cancelText={t('admin.users.cancel')}
        destroyOnClose
      >
        <Form
          form={resetForm}
          name="reset-suite-account-password"
          layout="vertical"
          style={{ marginTop: 16 }}
          initialValues={{ temporary: true }}
        >
          <Form.Item
            name="password"
            label={t('admin.users.fields.newPassword')}
            rules={[
              { required: true, message: t('admin.users.fields.passwordRequired') },
              { min: 8, message: t('admin.users.fields.passwordTooShort') },
            ]}
          >
            <Input.Password autoFocus autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="temporary" valuePropName="checked">
            <Checkbox>{t('admin.users.fields.temporaryPassword')}</Checkbox>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('admin.users.deleteTitle', { name: deleteTarget?.username ?? '' })}
        open={deleteTarget !== null}
        onCancel={() => setDeleteTarget(null)}
        onOk={onDelete}
        okText={t('admin.users.confirmDelete')}
        cancelText={t('admin.users.cancel')}
        okButtonProps={{ danger: true, disabled: deleteConfirmation !== deleteTarget?.username }}
        destroyOnClose
      >
        <Paragraph>{t('admin.users.deleteWarning', { name: deleteTarget?.username ?? '' })}</Paragraph>
        <Input
          autoFocus
          value={deleteConfirmation}
          placeholder={t('admin.users.deleteConfirmationPlaceholder')}
          onChange={(event) => setDeleteConfirmation(event.target.value)}
        />
      </Modal>
    </div>
  )
}
