// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * ProductRolesEditor edits a user's per-product tenant + role assignments.
 *
 * Each row maps to an identity-service group following the suite convention
 * /nereid{product}/{tenant}/{role}, so no product-specific logic lives here:
 * products are described by the catalog and tenants are loaded at runtime
 * through the NS gateway.
 */

import { useEffect, useState } from 'react'
import { AutoComplete, Button, Empty, Select, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import {
  ProductCatalogEntry,
  ProductRoleAssignment,
  ProductRoleAssignments,
  TenantOption,
  fetchProductTenants,
} from '../api/admin'

interface TenantState {
  loading: boolean
  options: TenantOption[]
  error: string | null
}

export interface ProductRolesEditorProps {
  catalog: ProductCatalogEntry[]
  value?: ProductRoleAssignments
  onChange?: (value: ProductRoleAssignments) => void
  disabled?: boolean
}

export default function ProductRolesEditor({ catalog, value, onChange, disabled }: ProductRolesEditorProps) {
  const { t } = useTranslation()
  const assignments = value ?? {}
  const [tenants, setTenants] = useState<Record<string, TenantState>>({})

  const loadTenants = async (product: ProductCatalogEntry) => {
    if (!product.tenants_path) return
    setTenants((prev) => ({ ...prev, [product.id]: { loading: true, options: prev[product.id]?.options ?? [], error: null } }))
    try {
      const options = await fetchProductTenants(product.id, product.tenants_path)
      setTenants((prev) => ({ ...prev, [product.id]: { loading: false, options, error: null } }))
    } catch {
      setTenants((prev) => ({
        ...prev,
        [product.id]: { loading: false, options: prev[product.id]?.options ?? [], error: t('admin.users.productRoles.tenantsFailed') },
      }))
    }
  }

  useEffect(() => {
    for (const product of catalog) {
      if (product.tenants_path && !tenants[product.id]) {
        void loadTenants(product)
      }
    }
    // Load once per product; refresh is manual.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalog])

  const roleLabel = (role: string) => {
    const key = `admin.users.productRoles.roleNames.${role}`
    const label = t(key)
    return label === key ? role : label
  }

  const update = (productId: string, rows: ProductRoleAssignment[]) => {
    const next: ProductRoleAssignments = { ...assignments }
    if (rows.length === 0) {
      delete next[productId]
    } else {
      next[productId] = rows
    }
    onChange?.(next)
  }

  if (catalog.length === 0) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('admin.users.productRoles.empty')} />
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {catalog.map((product) => {
        const rows = assignments[product.id] ?? []
        const state = tenants[product.id]
        const tenantOptions = (state?.options ?? []).map((tenant) => ({
          value: tenant.id,
          label: tenant.name === tenant.id ? tenant.id : `${tenant.name} (${tenant.id})`,
        }))
        const groupLabelId = `product-roles-${product.id}-label`
        return (
          <div
            key={product.id}
            role="group"
            aria-labelledby={groupLabelId}
            style={{ border: '1px solid #f0f0f0', borderRadius: 8, padding: 12 }}
          >
            <Space style={{ justifyContent: 'space-between', width: '100%', marginBottom: 8 }}>
              <Space size={6}>
                <Typography.Text id={groupLabelId} strong>{product.name}</Typography.Text>
                {product.tenants_path && state?.loading && <Spin size="small" />}
                {product.tenants_path && state?.error && (
                  <Tag color="orange">
                    {state.error}
                  </Tag>
                )}
              </Space>
              <Space size={4}>
                {product.tenants_path && (
                  <Tooltip title={t('admin.users.productRoles.reloadTenants')}>
                    <Button
                      size="small"
                      type="text"
                      icon={<ReloadOutlined />}
                      loading={state?.loading}
                      onClick={() => void loadTenants(product)}
                    />
                  </Tooltip>
                )}
                <Button
                  size="small"
                  icon={<PlusOutlined />}
                  disabled={disabled}
                  onClick={() =>
                    update(product.id, [...rows, { tenant_id: '', role: product.roles[0] ?? '' }])
                  }
                >
                  {t('admin.users.productRoles.add')}
                </Button>
              </Space>
            </Space>
            {rows.length === 0 ? (
              <Typography.Text type="secondary">{t('admin.users.productRoles.none')}</Typography.Text>
            ) : (
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                {rows.map((row, index) => (
                  <Space key={index} align="baseline" style={{ width: '100%' }}>
                    <AutoComplete
                      style={{ width: 240 }}
                      aria-label={`${product.name}: ${t('admin.users.productRoles.tenantPlaceholder')}`}
                      id={`product-${product.id}-tenant-${index}`}
                      value={row.tenant_id}
                      options={tenantOptions}
                      disabled={disabled}
                      placeholder={t('admin.users.productRoles.tenantPlaceholder')}
                      filterOption={(input, option) =>
                        String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
                      }
                      onChange={(tenantId) => {
                        const next = rows.slice()
                        next[index] = { ...row, tenant_id: tenantId }
                        update(product.id, next)
                      }}
                    />
                    <Select
                      style={{ width: 160 }}
                      aria-label={`${product.name}: ${t('admin.users.columns.roles')}`}
                      id={`product-${product.id}-role-${index}`}
                      value={row.role || undefined}
                      options={product.roles.map((role) => ({ value: role, label: roleLabel(role) }))}
                      disabled={disabled}
                      onChange={(role) => {
                        const next = rows.slice()
                        next[index] = { ...row, role }
                        update(product.id, next)
                      }}
                    />
                    <Button
                      size="small"
                      type="text"
                      danger
                      icon={<DeleteOutlined />}
                      aria-label={`${t('admin.users.delete')}: ${product.name}`}
                      disabled={disabled}
                      onClick={() => update(product.id, rows.filter((_, i) => i !== index))}
                    />
                  </Space>
                ))}
              </Space>
            )}
          </div>
        )
      })}
    </Space>
  )
}
