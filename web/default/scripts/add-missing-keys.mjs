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
import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    '{{count}} active requests': '{{count}} active requests',
    '{{count}} keys on shift': '{{count}} keys on shift',
    '{{count}} working': '{{count}} working',
    'Add API key group': 'Add API key group',
    'API key groups': 'API key groups',
    'API key activity office': 'API key activity office',
    'API Key Activity': 'API Key Activity',
    'API Key Office': 'API Key Office',
    'Auto group': 'Auto group',
    'Copy invitation code': 'Copy invitation code',
    'Copy invitation link': 'Copy invitation link',
    'Character {{number}}': 'Character {{number}}',
    'Choose how {{name}} appears in the API key office.':
      'Choose how {{name}} appears in the API key office.',
    'Create an API key and its character will appear here.':
      'Create an API key and its character will appear here.',
    'Display name must be at most 20 characters long':
      'Display name must be at most 20 characters long',
    'Drag to reorder': 'Drag to reorder',
    'Edit office character': 'Edit office character',
    'Failed to update office character': 'Failed to update office character',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Groups are tried from top to bottom. Billing uses the group that serves the request.',
    'Invitation code is required': 'Invitation code is required',
    'Invitation code reset successfully': 'Invitation code reset successfully',
    'Invitation Link': 'Invitation Link',
    'Invitation Required': 'Invitation Required',
    'Move {{group}} down': 'Move {{group}} down',
    'Move {{group}} up': 'Move {{group}} up',
    'No API keys in the office yet': 'No API keys in the office yet',
    'Office character updated': 'Office character updated',
    'Please select at least one group': 'Please select at least one group',
    'Priority {{priority}}': 'Priority {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Registration requires a valid invitation code or invite link.',
    'Require an invitation code or invite link before users can register':
      'Require an invitation code or invite link before users can register',
    'Remove {{group}}': 'Remove {{group}}',
    'Save character': 'Save character',
    'Showing your six most active API keys':
      'Showing your six most active API keys',
    Idle: 'Idle',
    Working: 'Working',
    'Shown publicly. Does not change your login username.':
      'Shown publicly. Does not change your login username.',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.',
    'You can select up to 20 groups': 'You can select up to 20 groups',
    'Your request content stays private': 'Your request content stays private',
  },
  zh: {
    '{{count}} active requests': '{{count}} 个活跃请求',
    '{{count}} keys on shift': '{{count}} 个密钥在岗',
    '{{count}} working': '{{count}} 个工作中',
    'Add API key group': '添加令牌分组',
    'API key groups': '令牌分组',
    'API key activity office': 'API 密钥劳动情况办公室',
    'API Key Activity': '密钥劳动情况',
    'API Key Office': 'API 密钥办公室',
    'Auto group': '自动分组',
    'Copy invitation code': '复制邀请码',
    'Copy invitation link': '复制邀请链接',
    'Character {{number}}': '角色 {{number}}',
    'Choose how {{name}} appears in the API key office.':
      '选择 {{name}} 在 API 密钥办公室中的角色形象。',
    'Create an API key and its character will appear here.':
      '创建 API 密钥后，它的角色就会出现在这里。',
    'Display name must be at most 20 characters long': '显示名称最多 20 个字符',
    'Drag to reorder': '拖拽排序',
    'Edit office character': '编辑角色形象',
    'Failed to update office character': '角色形象更新失败',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      '系统按从上到下的顺序尝试分组，并按实际处理请求的分组计费。',
    'Invitation code is required': '请输入邀请码',
    'Invitation code reset successfully': '邀请码已重置',
    'Invitation Link': '邀请链接',
    'Invitation Required': '需要邀请码',
    'Move {{group}} down': '将 {{group}} 下移',
    'Move {{group}} up': '将 {{group}} 上移',
    'No API keys in the office yet': '办公室里还没有 API 密钥',
    'Office character updated': '角色形象已更新',
    'Please select at least one group': '请至少选择一个分组',
    'Priority {{priority}}': '优先级 {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      '当前注册需要有效的邀请码或邀请链接。',
    'Require an invitation code or invite link before users can register':
      '要求用户必须通过邀请码或邀请链接才能注册',
    'Remove {{group}}': '移除 {{group}}',
    'Save character': '保存角色',
    'Showing your six most active API keys': '展示你最活跃的六个 API 密钥',
    Idle: '空闲',
    Working: '工作中',
    'Shown publicly. Does not change your login username.':
      '该名称会公开展示，不会修改登录用户名。',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      '此页面仅留存请求状态。平台不会存储请求内容，仅保留必要的 Token 用量统计和用户行为统计。',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '当所选分组均不支持请求模型时，继续按系统自动分配顺序查找。',
    'You can select up to 20 groups': '最多可选择 20 个分组',
    'Your request content stays private': '您的请求内容始终保密',
  },
  fr: {
    '{{count}} active requests': '{{count}} requêtes actives',
    '{{count}} keys on shift': '{{count}} clés en service',
    '{{count}} working': '{{count}} au travail',
    'Add API key group': 'Ajouter un groupe de clé API',
    'API key groups': 'Groupes de clés API',
    'API key activity office': "Bureau d'activité des clés API",
    'API Key Activity': 'Activité des clés API',
    'API Key Office': 'Bureau des clés API',
    'Auto group': 'Groupe automatique',
    'Copy invitation code': "Copier le code d'invitation",
    'Copy invitation link': "Copier le lien d'invitation",
    'Character {{number}}': 'Personnage {{number}}',
    'Choose how {{name}} appears in the API key office.':
      "Choisissez l'apparence de {{name}} dans le bureau des clés API.",
    'Create an API key and its character will appear here.':
      'Créez une clé API et son personnage apparaîtra ici.',
    'Display name must be at most 20 characters long':
      "Le nom d'affichage doit contenir au maximum 20 caractères",
    'Drag to reorder': 'Faire glisser pour réordonner',
    'Edit office character': 'Modifier le personnage',
    'Failed to update office character':
      'Échec de la mise à jour du personnage',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Les groupes sont essayés de haut en bas. La facturation utilise le groupe qui traite la requête.',
    'Invitation code is required': "Le code d'invitation est requis",
    'Invitation code reset successfully':
      "Le code d'invitation a été réinitialisé avec succès",
    'Invitation Link': "Lien d'invitation",
    'Invitation Required': 'Invitation requise',
    'Move {{group}} down': 'Descendre {{group}}',
    'Move {{group}} up': 'Monter {{group}}',
    'No API keys in the office yet':
      "Aucune clé API dans le bureau pour l'instant",
    'Office character updated': 'Personnage mis à jour',
    'Please select at least one group': 'Sélectionnez au moins un groupe',
    'Priority {{priority}}': 'Priorité {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      "L'inscription nécessite un code d'invitation valide ou un lien d'invitation.",
    'Require an invitation code or invite link before users can register':
      "Exiger un code d'invitation ou un lien d'invitation avant l'inscription",
    'Remove {{group}}': 'Retirer {{group}}',
    'Save character': 'Enregistrer le personnage',
    'Showing your six most active API keys':
      'Affichage de vos six clés API les plus actives',
    Idle: 'Inactif',
    Working: 'Au travail',
    'Shown publicly. Does not change your login username.':
      'Affiché publiquement. Ne modifie pas votre identifiant de connexion.',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      'Cette page conserve uniquement l’état des requêtes. La plateforme ne stocke pas leur contenu ; seules les statistiques nécessaires d’utilisation des tokens et d’activité utilisateur sont conservées.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      "Si le modèle demandé n'est disponible dans aucun groupe sélectionné, poursuivre selon l'ordre d'affectation automatique du système.",
    'You can select up to 20 groups':
      "Vous pouvez sélectionner jusqu'à 20 groupes",
    'Your request content stays private':
      'Le contenu de vos requêtes reste confidentiel',
  },
  ja: {
    '{{count}} active requests': 'アクティブなリクエスト {{count}} 件',
    '{{count}} keys on shift': '{{count}} 個のキーが勤務中',
    '{{count}} working': '{{count}} 個が作業中',
    'Add API key group': 'APIキーグループを追加',
    'API key groups': 'APIキーグループ',
    'API key activity office': 'APIキー稼働状況オフィス',
    'API Key Activity': 'APIキー稼働状況',
    'API Key Office': 'APIキーオフィス',
    'Auto group': '自動グループ',
    'Copy invitation code': '招待コードをコピー',
    'Copy invitation link': '招待リンクをコピー',
    'Character {{number}}': 'キャラクター {{number}}',
    'Choose how {{name}} appears in the API key office.':
      'APIキーオフィスでの {{name}} のキャラクターを選択します。',
    'Create an API key and its character will appear here.':
      'APIキーを作成すると、そのキャラクターがここに表示されます。',
    'Display name must be at most 20 characters long':
      '表示名は20文字以内で入力してください',
    'Drag to reorder': 'ドラッグして並べ替え',
    'Edit office character': 'キャラクターを編集',
    'Failed to update office character': 'キャラクターを更新できませんでした',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'グループは上から順に試行され、実際にリクエストを処理したグループで課金されます。',
    'Invitation code is required': '招待コードを入力してください',
    'Invitation code reset successfully': '招待コードをリセットしました',
    'Invitation Link': '招待リンク',
    'Invitation Required': '招待コード必須',
    'Move {{group}} down': '{{group}} を下へ移動',
    'Move {{group}} up': '{{group}} を上へ移動',
    'No API keys in the office yet': 'オフィスにAPIキーがありません',
    'Office character updated': 'キャラクターを更新しました',
    'Please select at least one group': 'グループを1つ以上選択してください',
    'Priority {{priority}}': '優先度 {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      '登録には有効な招待コードまたは招待リンクが必要です。',
    'Require an invitation code or invite link before users can register':
      'ユーザー登録の前に招待コードまたは招待リンクを必須にする',
    'Remove {{group}}': '{{group}} を削除',
    'Save character': 'キャラクターを保存',
    'Showing your six most active API keys':
      '最もアクティブな6個のAPIキーを表示しています',
    Idle: '待機中',
    Working: '作業中',
    'Shown publicly. Does not change your login username.':
      '公開表示されます。ログイン用ユーザー名は変更されません。',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      'このページに保存されるのはリクエストの状態だけです。プラットフォームはリクエスト内容を保存せず、必要なトークン使用量とユーザー行動の統計のみを保持します。',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '選択したグループで要求モデルを利用できない場合、システムの自動割り当て順序で検索を続けます。',
    'You can select up to 20 groups': '選択できるグループは最大20件です',
    'Your request content stays private': 'リクエスト内容は常に非公開です',
  },
  ru: {
    '{{count}} active requests': 'Активных запросов: {{count}}',
    '{{count}} keys on shift': 'Ключей на смене: {{count}}',
    '{{count}} working': 'Работают: {{count}}',
    'Add API key group': 'Добавить группу API-ключа',
    'API key groups': 'Группы API-ключа',
    'API key activity office': 'Офис активности API-ключей',
    'API Key Activity': 'Активность API-ключей',
    'API Key Office': 'Офис API-ключей',
    'Auto group': 'Автовыбор группы',
    'Copy invitation code': 'Скопировать код приглашения',
    'Copy invitation link': 'Скопировать ссылку приглашения',
    'Character {{number}}': 'Персонаж {{number}}',
    'Choose how {{name}} appears in the API key office.':
      'Выберите образ {{name}} в офисе API-ключей.',
    'Create an API key and its character will appear here.':
      'Создайте API-ключ, и его персонаж появится здесь.',
    'Display name must be at most 20 characters long':
      'Отображаемое имя должно быть не длиннее 20 символов',
    'Drag to reorder': 'Перетащите для изменения порядка',
    'Edit office character': 'Изменить персонажа',
    'Failed to update office character': 'Не удалось обновить персонажа',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Группы проверяются сверху вниз. Оплата рассчитывается по группе, обработавшей запрос.',
    'Invitation code is required': 'Требуется код приглашения',
    'Invitation code reset successfully': 'Код приглашения успешно сброшен',
    'Invitation Link': 'Ссылка приглашения',
    'Invitation Required': 'Требуется приглашение',
    'Move {{group}} down': 'Переместить {{group}} вниз',
    'Move {{group}} up': 'Переместить {{group}} вверх',
    'No API keys in the office yet': 'В офисе пока нет API-ключей',
    'Office character updated': 'Персонаж обновлён',
    'Please select at least one group': 'Выберите хотя бы одну группу',
    'Priority {{priority}}': 'Приоритет {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Для регистрации нужен действительный код приглашения или пригласительная ссылка.',
    'Require an invitation code or invite link before users can register':
      'Требовать код приглашения или ссылку перед регистрацией пользователей',
    'Remove {{group}}': 'Удалить {{group}}',
    'Save character': 'Сохранить персонажа',
    'Showing your six most active API keys':
      'Показаны шесть самых активных API-ключей',
    Idle: 'Неактивен',
    Working: 'Работает',
    'Shown publicly. Does not change your login username.':
      'Показывается публично. Не изменяет ваш логин.',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      'На этой странице сохраняется только статус запросов. Платформа не хранит их содержимое, а сохраняет только необходимую статистику использования токенов и действий пользователей.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'Если модель недоступна в выбранных группах, продолжить поиск по системному порядку автоназначения.',
    'You can select up to 20 groups': 'Можно выбрать не более 20 групп',
    'Your request content stays private':
      'Содержимое ваших запросов остаётся конфиденциальным',
  },
  vi: {
    '{{count}} active requests': '{{count}} yêu cầu đang hoạt động',
    '{{count}} keys on shift': '{{count}} khóa đang trực',
    '{{count}} working': '{{count}} khóa đang làm việc',
    'Add API key group': 'Thêm nhóm khóa API',
    'API key groups': 'Nhóm khóa API',
    'API key activity office': 'Văn phòng hoạt động khóa API',
    'API Key Activity': 'Hoạt động khóa API',
    'API Key Office': 'Văn phòng khóa API',
    'Auto group': 'Nhóm tự động',
    'Copy invitation code': 'Sao chép mã mời',
    'Copy invitation link': 'Sao chép liên kết mời',
    'Character {{number}}': 'Nhân vật {{number}}',
    'Choose how {{name}} appears in the API key office.':
      'Chọn hình ảnh của {{name}} trong văn phòng khóa API.',
    'Create an API key and its character will appear here.':
      'Tạo khóa API và nhân vật của khóa sẽ xuất hiện tại đây.',
    'Display name must be at most 20 characters long':
      'Tên hiển thị tối đa 20 ký tự',
    'Drag to reorder': 'Kéo để sắp xếp lại',
    'Edit office character': 'Chỉnh sửa nhân vật',
    'Failed to update office character': 'Không thể cập nhật nhân vật',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Các nhóm được thử từ trên xuống dưới. Chi phí được tính theo nhóm xử lý yêu cầu.',
    'Invitation code is required': 'Cần nhập mã mời',
    'Invitation code reset successfully': 'Đã đặt lại mã mời thành công',
    'Invitation Link': 'Liên kết mời',
    'Invitation Required': 'Yêu cầu mã mời',
    'Move {{group}} down': 'Di chuyển {{group}} xuống',
    'Move {{group}} up': 'Di chuyển {{group}} lên',
    'No API keys in the office yet': 'Chưa có khóa API trong văn phòng',
    'Office character updated': 'Đã cập nhật nhân vật',
    'Please select at least one group': 'Vui lòng chọn ít nhất một nhóm',
    'Priority {{priority}}': 'Ưu tiên {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Đăng ký yêu cầu mã mời hoặc liên kết mời hợp lệ.',
    'Require an invitation code or invite link before users can register':
      'Yêu cầu mã mời hoặc liên kết mời trước khi người dùng đăng ký',
    'Remove {{group}}': 'Xóa {{group}}',
    'Save character': 'Lưu nhân vật',
    'Showing your six most active API keys':
      'Hiển thị sáu khóa API hoạt động nhiều nhất',
    Idle: 'Đang rảnh',
    Working: 'Đang làm việc',
    'Shown publicly. Does not change your login username.':
      'Tên này sẽ hiển thị công khai và không làm thay đổi tên đăng nhập.',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      'Trang này chỉ lưu trạng thái yêu cầu. Nền tảng không lưu nội dung yêu cầu; chỉ các số liệu thống kê cần thiết về mức sử dụng token và hoạt động người dùng được lưu lại.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'Khi mô hình không có trong các nhóm đã chọn, tiếp tục theo thứ tự phân bổ tự động của hệ thống.',
    'You can select up to 20 groups': 'Bạn có thể chọn tối đa 20 nhóm',
    'Your request content stays private':
      'Nội dung yêu cầu của bạn luôn được bảo mật',
  },
  'zh-TW': {
    '{{count}} active requests': '{{count}} 個活躍請求',
    '{{count}} keys on shift': '{{count}} 個金鑰在崗',
    '{{count}} working': '{{count}} 個工作中',
    'Add API key group': '新增令牌分組',
    'API key groups': '令牌分組',
    'API key activity office': 'API 金鑰勞動情況辦公室',
    'API Key Activity': '金鑰勞動情況',
    'API Key Office': 'API 金鑰辦公室',
    'Character {{number}}': '角色 {{number}}',
    'Choose how {{name}} appears in the API key office.':
      '選擇 {{name}} 在 API 金鑰辦公室中的角色形象。',
    'Create an API key and its character will appear here.':
      '建立 API 金鑰後，它的角色就會出現在這裡。',
    'Auto group': '自動分組',
    'Drag to reorder': '拖曳排序',
    'Edit office character': '編輯角色形象',
    'Failed to update office character': '角色形象更新失敗',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      '系統會由上到下依序嘗試分組，並按實際處理請求的分組計費。',
    'Move {{group}} down': '將 {{group}} 下移',
    'Move {{group}} up': '將 {{group}} 上移',
    'No API keys in the office yet': '辦公室裡還沒有 API 金鑰',
    'Office character updated': '角色形象已更新',
    'Please select at least one group': '請至少選擇一個分組',
    'Priority {{priority}}': '優先級 {{priority}}',
    'Remove {{group}}': '移除 {{group}}',
    'Save character': '儲存角色',
    'Showing your six most active API keys': '顯示你最活躍的六個 API 金鑰',
    Idle: '空閒',
    Working: '工作中',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '當所選分組均不支援請求模型時，繼續按照系統自動分配順序查找。',
    'You can select up to 20 groups': '最多可選擇 20 個分組',
    'This page only retains request status. The platform does not store request content; only necessary token usage and user activity statistics are retained.':
      '此頁面僅留存請求狀態。平台不會儲存請求內容，僅保留必要的 Token 用量統計和使用者行為統計。',
    'Your request content stays private': '您的請求內容始終保密',
  },
}

async function main() {
  let totalAdded = 0

  for (const [locale, trans] of Object.entries(newKeys)) {
    const filePath = path.join(LOCALES_DIR, `${locale}.json`)
    const json = JSON.parse(await fs.readFile(filePath, 'utf8'))

    let count = 0
    for (const [key, value] of Object.entries(trans)) {
      if (!Object.prototype.hasOwnProperty.call(json.translation, key)) {
        json.translation[key] = value
        count++
      } else if (json.translation[key] !== value) {
        json.translation[key] = value
        count++
      }
    }

    if (count > 0) {
      json.translation = Object.fromEntries(
        Object.entries(json.translation).sort(([a], [b]) => a.localeCompare(b))
      )
      await fs.writeFile(filePath, stableStringify(json), 'utf8')
    }

    console.log(`${locale}: ${count} translations applied`)
    totalAdded += count
  }

  console.log(`\nTotal: ${totalAdded} translations applied`)
}

main().catch((err) => {
  console.error(err)
  process.exitCode = 1
})
