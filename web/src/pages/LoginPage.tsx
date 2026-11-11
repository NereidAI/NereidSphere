// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Suite login page.
 *
 * The rendered experience depends on the backend auth mode:
 *  - oidc: a single "Sign in with SSO" action navigates to /auth/login,
 *    which starts authorization-code + PKCE against NereidIdentity. The
 *    browser is redirected to the identity service's branded login page;
 *    NS never collects the password.
 *  - local: the emergency break-glass form posts to /api/v1/auth/login.
 *    These accounts come from the deployment Secret and are clearly labeled.
 * Error query parameters (?error=sso_denied|sso_failed|identity_unavailable)
 * surface the callback failures described in docs/sso-flow.md.
 */

import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Spin,
  Typography,
  App as AntdApp,
  Tooltip,
  Space,
} from 'antd'
import {
  LockOutlined,
  QuestionCircleOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { fetchAuthMethod, login, ssoLoginURL } from '../api/auth'
import { extractError } from '../api/client'
import LanguageSelect from '../components/LanguageSelect'
import { useHelpStore } from '../help/helpStore'

const { Title, Paragraph, Text } = Typography

/** LoginForm is the username/password payload in local mode. */
interface LoginForm {
  username: string
  password: string
}

/** LoginPage renders the SSO entry or the emergency local form. */
export default function LoginPage() {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)
  const [mode, setMode] = useState<'local' | 'oidc' | null>(null)
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const { message } = AntdApp.useApp()
  const openHelp = useHelpStore((state) => state.open)
  const redirect = params.get('redirect') ?? undefined
  const errorCode = params.get('error')

  useEffect(() => {
    let active = true
    fetchAuthMethod()
      .then((m) => active && setMode(m.mode))
      .catch(() => active && setMode('oidc'))
    return () => {
      active = false
    }
  }, [])

  const onFinish = async (values: LoginForm) => {
    setSubmitting(true)
    try {
      const result = await login(values.username, values.password, redirect)
      message.success(t('login.success'))
      if (/^https?:\/\//.test(result.redirect) || result.redirect.startsWith('/auth/')) {
        window.location.href = result.redirect
      } else {
        navigate(result.redirect || '/', { replace: true })
      }
    } catch (err) {
      const apiErr = extractError(err)
      const key =
        apiErr.code === 'INVALID_CREDENTIALS'
          ? 'login.error.invalidCredentials'
          : apiErr.code === 'RATE_LIMITED'
            ? 'login.error.rateLimited'
            : 'login.error.failed'
      message.error(t(key))
    } finally {
      setSubmitting(false)
    }
  }

  const errorAlert =
    errorCode === 'sso_denied'
      ? t('login.error.ssoDenied')
      : errorCode === 'identity_unavailable'
        ? t('login.error.identityUnavailable')
        : errorCode
          ? t('login.error.ssoFailed')
          : null

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #e6f0ff 0%, #f0f2f5 100%)',
        padding: 16,
      }}
    >
      <Card style={{ width: 420, position: 'relative' }} styles={{ body: { padding: '32px 32px 24px' } }}>
        <Space style={{ position: 'absolute', top: 12, right: 16 }} size={4}>
          <Tooltip title={t('common.help')}>
            <Button
              type="text"
              size="small"
              icon={<QuestionCircleOutlined />}
              onClick={() => openHelp('sso-login')}
            />
          </Tooltip>
          <LanguageSelect width={96} />
        </Space>

        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <SafetyCertificateOutlined style={{ fontSize: 44, color: '#1677ff' }} />
          <Title level={3} style={{ marginTop: 12, marginBottom: 4 }}>
            {t('login.title')}
          </Title>
          <Paragraph type="secondary" style={{ marginBottom: 0 }}>
            {t('login.subtitle')}
          </Paragraph>
        </div>

        {errorAlert && (
          <Alert type="error" showIcon message={errorAlert} style={{ marginBottom: 16 }} />
        )}

        {mode === null ? (
          <div style={{ textAlign: 'center', padding: '24px 0' }}>
            <Spin />
          </div>
        ) : mode === 'oidc' ? (
          <>
            <Paragraph type="secondary" style={{ textAlign: 'center' }}>
              {t('login.ssoHint')}
            </Paragraph>
            <Button
              type="primary"
              size="large"
              block
              href={ssoLoginURL(redirect)}
              icon={<SafetyCertificateOutlined />}
            >
              {t('login.ssoButton')}
            </Button>
          </>
        ) : (
          <>
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 16 }}
              message={t('login.emergencyTitle')}
              description={t('login.emergencyNote')}
            />
            <Form layout="vertical" onFinish={onFinish} autoComplete="off" requiredMark={false}>
              <Form.Item
                name="username"
                label={t('login.username')}
                rules={[{ required: true, message: t('login.usernameRequired') }]}
              >
                <Input size="large" prefix={<UserOutlined />} placeholder={t('login.usernamePlaceholder')} autoFocus />
              </Form.Item>
              <Form.Item
                name="password"
                label={t('login.password')}
                rules={[{ required: true, message: t('login.passwordRequired') }]}
              >
                <Input.Password
                  size="large"
                  prefix={<LockOutlined />}
                  placeholder={t('login.passwordPlaceholder')}
                />
              </Form.Item>
              <Form.Item style={{ marginBottom: 0, marginTop: 8 }}>
                <Button type="primary" htmlType="submit" size="large" block loading={submitting}>
                  {t('login.emergencySubmit')}
                </Button>
              </Form.Item>
            </Form>
            <div style={{ textAlign: 'center', marginTop: 12 }}>
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('login.emergencyFootnote')}
              </Text>
            </div>
          </>
        )}
      </Card>
    </div>
  )
}
