// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Identity connection status page.
 *
 * Shows the configured authentication mode, the NereidIdentity issuer and
 * its reachability, whether the Admin REST service-account client is
 * enabled, and the per-product integration mode (ns handoff ticket vs
 * standalone oidc). Read-only diagnostics for suite operators/admins.
 */

import { useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { QuestionCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useTranslation } from 'react-i18next'
import { adminStatus, ConnectionStatus } from '../api/admin'
import { useAuth } from '../hooks/useAuth'
import { useHelpStore } from '../help/helpStore'

const { Title, Paragraph, Text } = Typography

/** ConnectionStatusPage renders identity-plane diagnostics. */
export default function ConnectionStatusPage() {
  const { t } = useTranslation()
  const { identity, loading } = useAuth()
  const openHelp = useHelpStore((state) => state.open)
  const [status, setStatus] = useState<ConnectionStatus | null>(null)
  const [loadingStatus, setLoadingStatus] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const canRead =
    identity?.roles.includes('admin') || identity?.roles.includes('operator') || false

  const refresh = async () => {
    setLoadingStatus(true)
    setError(null)
    try {
      setStatus(await adminStatus())
    } catch {
      setError(t('admin.status.loadFailed'))
    } finally {
      setLoadingStatus(false)
    }
  }

  useEffect(() => {
    if (canRead) void refresh()
  }, [canRead])

  if (!loading && !identity) return <Navigate to="/login?redirect=%2Fadmin%2Fstatus" replace />
  if (!loading && !canRead) return <Navigate to="/" replace />

  const productColumns: ColumnsType<ConnectionStatus['products'][number]> = [
    { title: t('admin.status.productColumns.id'), dataIndex: 'id' },
    { title: t('admin.status.productColumns.name'), dataIndex: 'name' },
    {
      title: t('admin.status.productColumns.mode'),
      dataIndex: 'mode',
      render: (mode: string) => (
        <Tag color={mode === 'ns' ? 'blue' : 'geekblue'}>
          {mode === 'ns' ? t('admin.status.modeNS') : t('admin.status.modeOIDC')}
        </Tag>
      ),
    },
  ]

  return (
    <div style={{ maxWidth: 960, margin: '0 auto' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%', marginBottom: 16 }}>
        <div>
          <Title level={3} style={{ marginBottom: 4 }}>
            {t('admin.status.title')}
          </Title>
          <Paragraph type="secondary" style={{ marginBottom: 0 }}>
            {t('admin.status.subtitle')}
          </Paragraph>
        </div>
        <Space>
          <Tooltip title={t('common.help')}>
            <Button icon={<QuestionCircleOutlined />} onClick={() => openHelp('connection-status')} />
          </Tooltip>
          <Tooltip title={t('admin.status.refresh')}>
            <Button icon={<ReloadOutlined />} loading={loadingStatus} onClick={refresh} />
          </Tooltip>
        </Space>
      </Space>

      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}

      <Card style={{ marginBottom: 16 }}>
        <Descriptions column={1} bordered size="small">
          <Descriptions.Item label={t('admin.status.authMode')}>
            <Tag color={status?.mode === 'oidc' ? 'green' : 'orange'}>
              {status?.mode === 'oidc' ? t('admin.status.modeOIDC') : t('admin.status.modeLocal')}
            </Tag>
            {status?.mode === 'local' && <Text type="secondary"> {t('admin.status.localNote')}</Text>}
          </Descriptions.Item>
          <Descriptions.Item label={t('admin.status.issuer')}>
            {status?.issuer ? (
              <Text code>{status.issuer}</Text>
            ) : (
              <Text type="secondary">{t('admin.status.notConfigured')}</Text>
            )}
          </Descriptions.Item>
          <Descriptions.Item label={t('admin.status.issuerReady')}>
            {status?.mode === 'oidc' ? (
              <Tag color={status.issuer_ready ? 'green' : 'red'}>
                {status.issuer_ready ? t('admin.status.ready') : t('admin.status.unavailable')}
              </Tag>
            ) : (
              <Text type="secondary">—</Text>
            )}
          </Descriptions.Item>
          <Descriptions.Item label={t('admin.status.adminAPI')}>
            <Tag color={status?.admin_api ? 'green' : 'default'}>
              {status?.admin_api ? t('admin.status.enabled') : t('admin.status.disabled')}
            </Tag>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title={t('admin.status.productsTitle')}>
        <Table
          rowKey="id"
          pagination={false}
          columns={productColumns}
          dataSource={status?.products ?? []}
          loading={loadingStatus && !status}
        />
      </Card>
    </div>
  )
}
