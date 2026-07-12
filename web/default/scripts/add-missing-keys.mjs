import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    'Add API key group': 'Add API key group',
    'API key groups': 'API key groups',
    'Auto group': 'Auto group',
    'Copy invitation code': 'Copy invitation code',
    'Copy invitation link': 'Copy invitation link',
    'Display name must be at most 20 characters long':
      'Display name must be at most 20 characters long',
    'Drag to reorder': 'Drag to reorder',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Groups are tried from top to bottom. Billing uses the group that serves the request.',
    'Invitation code is required': 'Invitation code is required',
    'Invitation code reset successfully': 'Invitation code reset successfully',
    'Invitation Link': 'Invitation Link',
    'Invitation Required': 'Invitation Required',
    'Move {{group}} down': 'Move {{group}} down',
    'Move {{group}} up': 'Move {{group}} up',
    'Please select at least one group': 'Please select at least one group',
    'Priority {{priority}}': 'Priority {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Registration requires a valid invitation code or invite link.',
    'Require an invitation code or invite link before users can register':
      'Require an invitation code or invite link before users can register',
    'Remove {{group}}': 'Remove {{group}}',
    'Shown publicly. Does not change your login username.':
      'Shown publicly. Does not change your login username.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.',
    'You can select up to 20 groups': 'You can select up to 20 groups',
  },
  zh: {
    'Add API key group': '添加令牌分组',
    'API key groups': '令牌分组',
    'Auto group': '自动分组',
    'Copy invitation code': '复制邀请码',
    'Copy invitation link': '复制邀请链接',
    'Display name must be at most 20 characters long': '显示名称最多 20 个字符',
    'Drag to reorder': '拖拽排序',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      '系统按从上到下的顺序尝试分组，并按实际处理请求的分组计费。',
    'Invitation code is required': '请输入邀请码',
    'Invitation code reset successfully': '邀请码已重置',
    'Invitation Link': '邀请链接',
    'Invitation Required': '需要邀请码',
    'Move {{group}} down': '将 {{group}} 下移',
    'Move {{group}} up': '将 {{group}} 上移',
    'Please select at least one group': '请至少选择一个分组',
    'Priority {{priority}}': '优先级 {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      '当前注册需要有效的邀请码或邀请链接。',
    'Require an invitation code or invite link before users can register':
      '要求用户必须通过邀请码或邀请链接才能注册',
    'Remove {{group}}': '移除 {{group}}',
    'Shown publicly. Does not change your login username.':
      '该名称会公开展示，不会修改登录用户名。',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '当所选分组均不支持请求模型时，继续按系统自动分配顺序查找。',
    'You can select up to 20 groups': '最多可选择 20 个分组',
  },
  fr: {
    'Add API key group': 'Ajouter un groupe de clé API',
    'API key groups': 'Groupes de clés API',
    'Auto group': 'Groupe automatique',
    'Copy invitation code': "Copier le code d'invitation",
    'Copy invitation link': "Copier le lien d'invitation",
    'Display name must be at most 20 characters long':
      "Le nom d'affichage doit contenir au maximum 20 caractères",
    'Drag to reorder': 'Faire glisser pour réordonner',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Les groupes sont essayés de haut en bas. La facturation utilise le groupe qui traite la requête.',
    'Invitation code is required': "Le code d'invitation est requis",
    'Invitation code reset successfully':
      "Le code d'invitation a été réinitialisé avec succès",
    'Invitation Link': "Lien d'invitation",
    'Invitation Required': 'Invitation requise',
    'Move {{group}} down': 'Descendre {{group}}',
    'Move {{group}} up': 'Monter {{group}}',
    'Please select at least one group': 'Sélectionnez au moins un groupe',
    'Priority {{priority}}': 'Priorité {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      "L'inscription nécessite un code d'invitation valide ou un lien d'invitation.",
    'Require an invitation code or invite link before users can register':
      "Exiger un code d'invitation ou un lien d'invitation avant l'inscription",
    'Remove {{group}}': 'Retirer {{group}}',
    'Shown publicly. Does not change your login username.':
      'Affiché publiquement. Ne modifie pas votre identifiant de connexion.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      "Si le modèle demandé n'est disponible dans aucun groupe sélectionné, poursuivre selon l'ordre d'affectation automatique du système.",
    'You can select up to 20 groups':
      "Vous pouvez sélectionner jusqu'à 20 groupes",
  },
  ja: {
    'Add API key group': 'APIキーグループを追加',
    'API key groups': 'APIキーグループ',
    'Auto group': '自動グループ',
    'Copy invitation code': '招待コードをコピー',
    'Copy invitation link': '招待リンクをコピー',
    'Display name must be at most 20 characters long':
      '表示名は20文字以内で入力してください',
    'Drag to reorder': 'ドラッグして並べ替え',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'グループは上から順に試行され、実際にリクエストを処理したグループで課金されます。',
    'Invitation code is required': '招待コードを入力してください',
    'Invitation code reset successfully': '招待コードをリセットしました',
    'Invitation Link': '招待リンク',
    'Invitation Required': '招待コード必須',
    'Move {{group}} down': '{{group}} を下へ移動',
    'Move {{group}} up': '{{group}} を上へ移動',
    'Please select at least one group': 'グループを1つ以上選択してください',
    'Priority {{priority}}': '優先度 {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      '登録には有効な招待コードまたは招待リンクが必要です。',
    'Require an invitation code or invite link before users can register':
      'ユーザー登録の前に招待コードまたは招待リンクを必須にする',
    'Remove {{group}}': '{{group}} を削除',
    'Shown publicly. Does not change your login username.':
      '公開表示されます。ログイン用ユーザー名は変更されません。',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '選択したグループで要求モデルを利用できない場合、システムの自動割り当て順序で検索を続けます。',
    'You can select up to 20 groups': '選択できるグループは最大20件です',
  },
  ru: {
    'Add API key group': 'Добавить группу API-ключа',
    'API key groups': 'Группы API-ключа',
    'Auto group': 'Автовыбор группы',
    'Copy invitation code': 'Скопировать код приглашения',
    'Copy invitation link': 'Скопировать ссылку приглашения',
    'Display name must be at most 20 characters long':
      'Отображаемое имя должно быть не длиннее 20 символов',
    'Drag to reorder': 'Перетащите для изменения порядка',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Группы проверяются сверху вниз. Оплата рассчитывается по группе, обработавшей запрос.',
    'Invitation code is required': 'Требуется код приглашения',
    'Invitation code reset successfully': 'Код приглашения успешно сброшен',
    'Invitation Link': 'Ссылка приглашения',
    'Invitation Required': 'Требуется приглашение',
    'Move {{group}} down': 'Переместить {{group}} вниз',
    'Move {{group}} up': 'Переместить {{group}} вверх',
    'Please select at least one group': 'Выберите хотя бы одну группу',
    'Priority {{priority}}': 'Приоритет {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Для регистрации нужен действительный код приглашения или пригласительная ссылка.',
    'Require an invitation code or invite link before users can register':
      'Требовать код приглашения или ссылку перед регистрацией пользователей',
    'Remove {{group}}': 'Удалить {{group}}',
    'Shown publicly. Does not change your login username.':
      'Показывается публично. Не изменяет ваш логин.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'Если модель недоступна в выбранных группах, продолжить поиск по системному порядку автоназначения.',
    'You can select up to 20 groups': 'Можно выбрать не более 20 групп',
  },
  vi: {
    'Add API key group': 'Thêm nhóm khóa API',
    'API key groups': 'Nhóm khóa API',
    'Auto group': 'Nhóm tự động',
    'Copy invitation code': 'Sao chép mã mời',
    'Copy invitation link': 'Sao chép liên kết mời',
    'Display name must be at most 20 characters long':
      'Tên hiển thị tối đa 20 ký tự',
    'Drag to reorder': 'Kéo để sắp xếp lại',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      'Các nhóm được thử từ trên xuống dưới. Chi phí được tính theo nhóm xử lý yêu cầu.',
    'Invitation code is required': 'Cần nhập mã mời',
    'Invitation code reset successfully': 'Đã đặt lại mã mời thành công',
    'Invitation Link': 'Liên kết mời',
    'Invitation Required': 'Yêu cầu mã mời',
    'Move {{group}} down': 'Di chuyển {{group}} xuống',
    'Move {{group}} up': 'Di chuyển {{group}} lên',
    'Please select at least one group': 'Vui lòng chọn ít nhất một nhóm',
    'Priority {{priority}}': 'Ưu tiên {{priority}}',
    'Registration requires a valid invitation code or invite link.':
      'Đăng ký yêu cầu mã mời hoặc liên kết mời hợp lệ.',
    'Require an invitation code or invite link before users can register':
      'Yêu cầu mã mời hoặc liên kết mời trước khi người dùng đăng ký',
    'Remove {{group}}': 'Xóa {{group}}',
    'Shown publicly. Does not change your login username.':
      'Tên này sẽ hiển thị công khai và không làm thay đổi tên đăng nhập.',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      'Khi mô hình không có trong các nhóm đã chọn, tiếp tục theo thứ tự phân bổ tự động của hệ thống.',
    'You can select up to 20 groups': 'Bạn có thể chọn tối đa 20 nhóm',
  },
  'zh-TW': {
    'Add API key group': '新增令牌分組',
    'API key groups': '令牌分組',
    'Auto group': '自動分組',
    'Drag to reorder': '拖曳排序',
    'Groups are tried from top to bottom. Billing uses the group that serves the request.':
      '系統會由上到下依序嘗試分組，並按實際處理請求的分組計費。',
    'Move {{group}} down': '將 {{group}} 下移',
    'Move {{group}} up': '將 {{group}} 上移',
    'Please select at least one group': '請至少選擇一個分組',
    'Priority {{priority}}': '優先級 {{priority}}',
    'Remove {{group}}': '移除 {{group}}',
    'When the requested model is unavailable in the selected groups, continue with the system auto assignment order.':
      '當所選分組均不支援請求模型時，繼續按照系統自動分配順序查找。',
    'You can select up to 20 groups': '最多可選擇 20 個分組',
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
