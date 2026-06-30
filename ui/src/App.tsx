import { useState } from 'react'
import CommitPR from './components/CommitPR'
import DriftView from './components/DriftView'
import Layout from './components/Layout'
import PlanPreview from './components/PlanPreview'
import ResourceList from './components/ResourceList'
import type { ResourceSpec, Tab } from './types'

export default function App() {
  const [tab, setTab] = useState<Tab>('resources')
  const [specs, setSpecs] = useState<ResourceSpec[]>([])
  const [provider, setProvider] = useState('digitalocean')

  return (
    <Layout tab={tab} onTabChange={setTab}>
      {tab === 'resources' && (
        <ResourceList
          onSpecsChange={setSpecs}
          onProviderChange={setProvider}
        />
      )}
      {tab === 'plan' && (
        <PlanPreview
          provider={provider}
          specs={specs}
          onRequestCommitTab={() => setTab('commit')}
        />
      )}
      {tab === 'commit' && <CommitPR specs={specs} />}
      {tab === 'drift' && <DriftView provider={provider} />}
    </Layout>
  )
}
