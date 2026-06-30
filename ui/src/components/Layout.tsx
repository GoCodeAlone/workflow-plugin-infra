import type { Tab } from '../types'

const TAB_LABELS: Record<Tab, string> = {
  resources: 'Resources',
  plan: 'Plan',
  commit: 'Commit / PR',
  drift: 'Drift',
}

interface LayoutProps {
  tab: Tab
  onTabChange: (tab: Tab) => void
  children: React.ReactNode
}

export default function Layout({ tab, onTabChange, children }: LayoutProps) {
  const tabs: Tab[] = ['resources', 'plan', 'commit', 'drift']
  return (
    <div className="app">
      <header className="app-header">
        <h1>Infrastructure</h1>
      </header>
      <nav className="tab-nav">
        {tabs.map((t) => (
          <button
            key={t}
            className={`tab-btn${tab === t ? ' active' : ''}`}
            onClick={() => onTabChange(t)}
          >
            {TAB_LABELS[t]}
          </button>
        ))}
      </nav>
      <main className="tab-content">{children}</main>
    </div>
  )
}
