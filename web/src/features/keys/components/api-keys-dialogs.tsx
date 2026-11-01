import { useTranslation } from 'react-i18next'

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Dialog } from '@/components/dialog'
import { HostingClientConfig } from '@/features/hosting/components/client-config'

import { ApiKeysDeleteDialog } from './api-keys-delete-dialog'
import { ApiKeysMutateDrawer } from './api-keys-mutate-drawer'
import { useApiKeys } from './api-keys-provider'
import { CCSwitchDialog } from './dialogs/cc-switch-dialog'

export function ApiKeysDialogs() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, resolvedKey } = useApiKeys()

  return (
    <>
      <ApiKeysMutateDrawer
        open={open === 'create' || open === 'update'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        currentRow={open === 'update' ? currentRow || undefined : undefined}
      />
      <ApiKeysDeleteDialog />
      {/*
       * 模型配置从当前 Key 菜单进入，明文读取仍是单独的用户操作。
       * 关闭后卸载组件，清除模型选择和已经展示的密钥。
       * Key 编号变化必须重挂载，不能让旧 mutation 结果污染新配置。
       * 两种菜单共享当前行来源，不再次加载并选择另一个 Key。
       */}
      {open === 'client-config' && currentRow && (
        <Dialog
          open
          onOpenChange={(isOpen) => !isOpen && setOpen(null)}
          title={t('Codex configuration')}
          description={currentRow.name}
        >
          <HostingClientConfig key={currentRow.id} tokenId={currentRow.id} />
        </Dialog>
      )}
      <CCSwitchDialog
        open={open === 'cc-switch'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        tokenKey={resolvedKey}
        tokenId={currentRow?.id}
      />
    </>
  )
}
