import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

function stableStringify(obj) {
  return JSON.stringify(obj, null, 2) + '\n'
}

const newKeys = {
  en: {
    'Copy invitation code': 'Copy invitation code',
    'Copy invitation link': 'Copy invitation link',
    'Display name must be at most 20 characters long':
      'Display name must be at most 20 characters long',
    'Invitation code is required': 'Invitation code is required',
    'Invitation code reset successfully':
      'Invitation code reset successfully',
    'Invitation Link': 'Invitation Link',
    'Invitation Required': 'Invitation Required',
    'Registration requires a valid invitation code or invite link.':
      'Registration requires a valid invitation code or invite link.',
    'Require an invitation code or invite link before users can register':
      'Require an invitation code or invite link before users can register',
    'Shown publicly. Does not change your login username.':
      'Shown publicly. Does not change your login username.',
  },
  zh: {
    'Copy invitation code': '复制邀请码',
    'Copy invitation link': '复制邀请链接',
    'Display name must be at most 20 characters long': '显示名称最多 20 个字符',
    'Invitation code is required': '请输入邀请码',
    'Invitation code reset successfully': '邀请码已重置',
    'Invitation Link': '邀请链接',
    'Invitation Required': '需要邀请码',
    'Registration requires a valid invitation code or invite link.':
      '当前注册需要有效的邀请码或邀请链接。',
    'Require an invitation code or invite link before users can register':
      '要求用户必须通过邀请码或邀请链接才能注册',
    'Shown publicly. Does not change your login username.':
      '该名称会公开展示，不会修改登录用户名。',
  },
  fr: {
    'Copy invitation code': "Copier le code d'invitation",
    'Copy invitation link': "Copier le lien d'invitation",
    'Display name must be at most 20 characters long':
      "Le nom d'affichage doit contenir au maximum 20 caractères",
    'Invitation code is required': "Le code d'invitation est requis",
    'Invitation code reset successfully':
      "Le code d'invitation a été réinitialisé avec succès",
    'Invitation Link': "Lien d'invitation",
    'Invitation Required': 'Invitation requise',
    'Registration requires a valid invitation code or invite link.':
      "L'inscription nécessite un code d'invitation valide ou un lien d'invitation.",
    'Require an invitation code or invite link before users can register':
      "Exiger un code d'invitation ou un lien d'invitation avant l'inscription",
    'Shown publicly. Does not change your login username.':
      "Affiché publiquement. Ne modifie pas votre identifiant de connexion.",
  },
  ja: {
    'Copy invitation code': '招待コードをコピー',
    'Copy invitation link': '招待リンクをコピー',
    'Display name must be at most 20 characters long':
      '表示名は20文字以内で入力してください',
    'Invitation code is required': '招待コードを入力してください',
    'Invitation code reset successfully': '招待コードをリセットしました',
    'Invitation Link': '招待リンク',
    'Invitation Required': '招待コード必須',
    'Registration requires a valid invitation code or invite link.':
      '登録には有効な招待コードまたは招待リンクが必要です。',
    'Require an invitation code or invite link before users can register':
      'ユーザー登録の前に招待コードまたは招待リンクを必須にする',
    'Shown publicly. Does not change your login username.':
      '公開表示されます。ログイン用ユーザー名は変更されません。',
  },
  ru: {
    'Copy invitation code': 'Скопировать код приглашения',
    'Copy invitation link': 'Скопировать ссылку приглашения',
    'Display name must be at most 20 characters long':
      'Отображаемое имя должно быть не длиннее 20 символов',
    'Invitation code is required': 'Требуется код приглашения',
    'Invitation code reset successfully': 'Код приглашения успешно сброшен',
    'Invitation Link': 'Ссылка приглашения',
    'Invitation Required': 'Требуется приглашение',
    'Registration requires a valid invitation code or invite link.':
      'Для регистрации нужен действительный код приглашения или пригласительная ссылка.',
    'Require an invitation code or invite link before users can register':
      'Требовать код приглашения или ссылку перед регистрацией пользователей',
    'Shown publicly. Does not change your login username.':
      'Показывается публично. Не изменяет ваш логин.',
  },
  vi: {
    'Copy invitation code': 'Sao chép mã mời',
    'Copy invitation link': 'Sao chép liên kết mời',
    'Display name must be at most 20 characters long':
      'Tên hiển thị tối đa 20 ký tự',
    'Invitation code is required': 'Cần nhập mã mời',
    'Invitation code reset successfully': 'Đã đặt lại mã mời thành công',
    'Invitation Link': 'Liên kết mời',
    'Invitation Required': 'Yêu cầu mã mời',
    'Registration requires a valid invitation code or invite link.':
      'Đăng ký yêu cầu mã mời hoặc liên kết mời hợp lệ.',
    'Require an invitation code or invite link before users can register':
      'Yêu cầu mã mời hoặc liên kết mời trước khi người dùng đăng ký',
    'Shown publicly. Does not change your login username.':
      'Tên này sẽ hiển thị công khai và không làm thay đổi tên đăng nhập.',
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
