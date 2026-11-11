// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Suite portal landing page.
 *
 * It resolves the current identity and the product catalogue on load.
 * Unauthenticated visitors are redirected to the NS-owned /login page; after
 * signing in, the product grid offers deep links that carry the established
 * single sign-on session so each product opens without another login.
 */

import { useEffect, useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import {
  Layout,
  Row,
  Col,
  Card,
  Typography,
  Space,
  Spin,
  Dropdown,
  Avatar,
  Tag,
  Button,
  Tooltip,
  Alert,
  App as AntdApp,
} from 'antd'
import type { MenuProps } from 'antd'
import {
  ApiOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
  LockOutlined,
  LogoutOutlined,
  QuestionCircleOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../hooks/useAuth'
import { listProducts, Product } from '../api/auth'
import LanguageSelect from '../components/LanguageSelect'

const { Header, Content, Footer } = Layout
const { Title, Text, Paragraph } = Typography

/** productIcon maps a product id to its portal icon. */
function productIcon(id: string) {
  const style = { fontSize: 30, color: '#1677ff' }
  switch (id) {
    case 'link':
      return <ApiOutlined style={style} />
    case 'forge':
      return <DatabaseOutlined style={style} />
    case 'horizon':
      return <CloudServerOutlined style={style} />
    default:
      return <SafetyCertificateOutlined style={style} />
  }
}

/** PortalPage renders the authenticated product grid. */
export default function PortalPage() {
  const { t } = useTranslation()
  const { identity, loading } = useAuth()
  const [products, setProducts] = useState<Product[]>([])
  const location = useLocation()
  const navigate = useNavigate()
  const { message } = AntdApp.useApp()

  useEffect(() => {
    listProducts()
      .then(setProducts)
      .catch(() => message.error(t('portal.catalogError')))
  }, [message, t])

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Spin size="large" tip={t('common.loading')}>
          <div style={{ padding: 50 }} />
        </Spin>
      </div>
    )
  }

  // Gate the portal behind the NS-owned login page, carrying the requested
  // destination as an allowlist-checked redirect hint.
  if (!identity) {
    const returnTo = location.pathname + location.search
    return <Navigate to={`/login?redirect=${encodeURIComponent(returnTo)}`} replace />
  }

  const onLogout = () => {
    // Browser-followable RP logout: clears the NS session and continues to
    // the identity service end-session endpoint, then back to the portal.
    window.location.href = '/auth/logout'
  }

  // /auth/enter bounces unassigned users back here with a product_error so
  // hand-typed launch URLs get an explanation instead of a silent loop.
  const search = new URLSearchParams(location.search)
  const deniedID = search.get('product_error') === 'not_assigned'
    ? search.get('product')
    : null
  const deniedName =
    products.find((p) => p.id === deniedID)?.name ?? deniedID ?? ''

  const canAdmin =
    identity.roles.includes('admin') || identity.roles.includes('operator')

  // Keep API catalogue text as a fallback for products added by a site, while
  // shipping bilingual copy for the built-in closed-loop product entries.
  const productDescription = (product: Product) => {
    switch (product.id) {
      case 'forge':
        return t('portal.products.forge')
      case 'horizon':
        return t('portal.products.horizon')
      case 'link':
        return t('portal.products.link')
      default:
        return product.description
    }
  }

  const roleLabel = (role: string) => {
    switch (role) {
      case 'admin':
        return t('portal.roles.admin')
      case 'operator':
        return t('portal.roles.operator')
      case 'user':
        return t('portal.roles.user')
      default:
        return role
    }
  }

  const userMenu: MenuProps['items'] = [
    ...(canAdmin
      ? [
          {
            key: 'admin-users',
            icon: <SafetyCertificateOutlined />,
            label: t('userMenu.adminUsers'),
            onClick: () => navigate('/admin/users'),
          },
          {
            key: 'admin-status',
            icon: <CloudServerOutlined />,
            label: t('userMenu.connectionStatus'),
            onClick: () => navigate('/admin/status'),
          },
        ]
      : []),
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: t('userMenu.signOut'),
      onClick: onLogout,
    },
  ]

  return (
    <Layout style={{ minHeight: '100vh', background: '#f0f2f5' }}>
      <Header
        style={{
          background: '#fff',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 32px',
          boxShadow: '0 1px 4px rgba(0,21,41,.08)',
        }}
      >
        <Space align="center" size="middle">
          <SafetyCertificateOutlined style={{ fontSize: 24, color: '#1677ff' }} />
          <Title level={3} style={{ margin: 0 }}>
            {t('nav.suiteName')}
          </Title>
          <Text type="secondary">{t('nav.suiteTagline')}</Text>
        </Space>
        <Space size="middle">
          <Tooltip title={t('nav.help')}>
            <Button
              type="text"
              icon={<QuestionCircleOutlined />}
              onClick={() => navigate('/help')}
            />
          </Tooltip>
          <LanguageSelect />
          <Dropdown menu={{ items: userMenu }} placement="bottomRight">
            <Space style={{ cursor: 'pointer' }}>
              <Avatar icon={<UserOutlined />} />
              <span>{identity.name || identity.preferred_username || identity.email}</span>
            </Space>
          </Dropdown>
        </Space>
      </Header>

      <Content style={{ padding: '48px 32px' }}>
        <div style={{ maxWidth: 1100, margin: '0 auto' }}>
          <div style={{ textAlign: 'center', marginBottom: 40 }}>
            <Title level={2} style={{ marginBottom: 8 }}>
              {identity.name
                ? `${t('portal.welcome')}, ${identity.name}`
                : t('portal.welcome')}
            </Title>
            <Paragraph type="secondary" style={{ marginBottom: 0 }}>
              {t('portal.ssoActive')}
            </Paragraph>
            <Space style={{ marginTop: 12 }} wrap>
              {identity.roles.map((role) => (
                <Tag color="blue" key={role}>
                  {roleLabel(role)}
                </Tag>
              ))}
            </Space>
          </div>
          {deniedID && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 24 }}
              message={t('portal.notAssignedAlert', { product: deniedName })}
            />
          )}
          <Row gutter={[24, 24]}>
            {products.map((product) => (
              <Col xs={24} sm={12} lg={8} key={product.id}>
                <Tooltip title={product.assigned ? undefined : t('portal.notAssignedTip')}>
                  <Card
                    hoverable={product.assigned}
                    onClick={() => {
                      if (product.assigned) {
                        window.open(product.launch_url, '_blank', 'noopener,noreferrer')
                      }
                    }}
                    style={
                      product.assigned
                        ? undefined
                        : { opacity: 0.55, cursor: 'not-allowed', background: '#fafafa' }
                    }
                  >
                    <Card.Meta
                      avatar={productIcon(product.id)}
                      title={
                        <Space size={8} wrap>
                          <span>{product.name}</span>
                          {!product.assigned && (
                            <Tag icon={<LockOutlined />} color="default">
                              {t('portal.notAssignedTag')}
                            </Tag>
                          )}
                        </Space>
                      }
                      description={productDescription(product)}
                    />
                  </Card>
                </Tooltip>
              </Col>
            ))}
          </Row>
        </div>
      </Content>

      <Footer style={{ textAlign: 'center', background: 'transparent' }}>
        {t('portal.footer')}
      </Footer>
    </Layout>
  )
}
