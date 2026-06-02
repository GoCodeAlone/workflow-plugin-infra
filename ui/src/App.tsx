import { useState } from 'react'
import CommitPR from './components/CommitPR'
import DriftView from './components/DriftView'
import Layout from './components/Layout'
import PlanPreview from './components/PlanPreview'
import ResourceList from './components/ResourceList'
import SecretsPanel from './components/SecretsPanel'
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
        />
      )}
      {tab === 'commit' && <CommitPR specs={specs} />}
      {tab === 'drift' && <DriftView provider={provider} />}
      {tab === 'secrets' && <SecretsPanel />}
    </Layout>
  )
}
