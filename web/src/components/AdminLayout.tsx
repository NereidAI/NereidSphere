// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Minimal authenticated chrome for admin pages: the same suite header as the
 * portal with navigation back to the product grid, the identity status page
 * and the user menu. Route guards live in the individual pages.
 */

import { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Layout,
  Typography,
  Space,
  Button,
  Dropdown,
  Avatar,
  Tooltip,
} from 'antd'
import type { MenuProps } from 'antd'
import {
  AppstoreOutlined,
  LogoutOutlined,
  QuestionCircleOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../hooks/useAuth'
import LanguageSelect from './LanguageSelect'

const { Header, Content, Footer } = Layout
const { Title, Text } = Typography

/** AdminLayout frames the identity administration pages. */
export default function AdminLayout({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { identity } = useAuth()
  const navigate = useNavigate()

  const onLogout = () => {
    window.location.href = '/auth/logout'
  }

  const userMenu: MenuProps['items'] = [
    { key: 'portal', icon: <AppstoreOutlined />, label: t('admin.nav.portal'), onClick: () => navigate('/') },
    { key: 'logout', icon: <LogoutOutlined />, label: t('userMenu.signOut'), onClick: onLogout },
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
          <Text type="secondary">{t('admin.nav.title')}</Text>
        </Space>
        <Space size="middle">
          <Button type="text" onClick={() => navigate('/admin/status')}>
            {t('admin.nav.status')}
          </Button>
          <Button type="text" onClick={() => navigate('/admin/users')}>
            {t('admin.nav.users')}
          </Button>
          <Tooltip title={t('nav.help')}>
            <Button type="text" icon={<QuestionCircleOutlined />} onClick={() => navigate('/help')} />
          </Tooltip>
          <LanguageSelect />
          <Dropdown menu={{ items: userMenu }} placement="bottomRight">
            <Space style={{ cursor: 'pointer' }}>
              <Avatar icon={<UserOutlined />} />
              <span>{identity?.name || identity?.preferred_username || identity?.email}</span>
            </Space>
          </Dropdown>
        </Space>
      </Header>
      <Content style={{ padding: '32px' }}>{children}</Content>
      <Footer style={{ textAlign: 'center', background: 'transparent' }}>
        {t('portal.footer')}
      </Footer>
    </Layout>
  )
}
