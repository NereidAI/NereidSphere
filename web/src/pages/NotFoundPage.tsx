// Copyright (C) 2026 YIQISOFT
// SPDX-License-Identifier: Apache-2.0

/**
 * Standalone 404 page for unknown SPA routes. Known deep links (/login,
 * /help/..., the portal root) have their own routes.
 */

import { Button, Result } from 'antd'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

/** NotFoundPage renders the localized 404 result. */
export default function NotFoundPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Result
      status="404"
      title={t('notFound.title')}
      subTitle={t('notFound.subtitle')}
      extra={
        <Button type="primary" onClick={() => navigate('/', { replace: true })}>
          {t('notFound.backHome')}
        </Button>
      }
    />
  )
}
