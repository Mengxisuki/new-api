import { createFileRoute } from '@tanstack/react-router'

import { KeyOfficeCanvas } from '@/features/dashboard/components/key-office/key-office-canvas'

export const Route = createFileRoute('/key-office-preview')({
  component: Preview,
})

function Preview() {
  return (
    <main className='min-h-screen bg-neutral-100 p-6 dark:bg-neutral-950'>
      <section className='mx-auto max-w-6xl overflow-hidden rounded-xl border bg-[#f7f3e8] shadow-sm dark:bg-[#171b18]'>
        <header className='border-b border-black/5 px-5 py-4 dark:border-white/10'>
          <h1 className='font-semibold'>API 密钥办公室</h1>
          <p className='text-muted-foreground text-sm'>展示你最活跃的六个 API 密钥</p>
        </header>
        <KeyOfficeCanvas
          employees={[
            { id: 1, name: '生产环境', status: 1, office_character: 'misaki', active_requests: 2, requests_24h: 96, last_used_at: 1, state: 'working' },
            { id: 2, name: '数据分析', status: 1, office_character: 'nate', active_requests: 0, requests_24h: 42, last_used_at: 1, state: 'idle' },
            { id: 3, name: '测试密钥', status: 1, office_character: 'luke', active_requests: 0, requests_24h: 8, last_used_at: 1, state: 'idle' },
          ]}
        />
      </section>
    </main>
  )
}
