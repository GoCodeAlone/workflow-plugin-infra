import { useEffect, useState } from 'react'
import { getProviderCatalog, listResources } from '../api'
import type { ProviderCatalog, Resource, ResourceSpec } from '../types'

const PROVIDERS = ['digitalocean', 'aws', 'gcp', 'azure']

interface ResourceListProps {
  onSpecsChange?: (specs: ResourceSpec[]) => void
  onProviderChange?: (provider: string) => void
}

export default function ResourceList({ onSpecsChange, onProviderChange }: ResourceListProps) {
  const [provider, setProvider] = useState(PROVIDERS[0])
  const [resources, setResources] = useState<Resource[]>([])
  const [catalog, setCatalog] = useState<ProviderCatalog | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Editor form state — all dropdowns fed from catalog
  const [editName, setEditName] = useState('')
  const [editType, setEditType] = useState('')
  const [editRegion, setEditRegion] = useState('')
  const [editProvider, setEditProvider] = useState(PROVIDERS[0])
  const [specs, setSpecs] = useState<ResourceSpec[]>([])

  useEffect(() => {
    onProviderChange?.(provider)
    setLoading(true)
    setError(null)
    Promise.all([listResources(provider), getProviderCatalog(provider)])
      .then(([res, cat]) => {
        setResources(res.resources ?? [])
        setCatalog(cat)
        // Reset form dropdowns to first catalog value when provider changes
        if (cat.types.length > 0) setEditType(cat.types[0])
        if (cat.regions.length > 0) setEditRegion(cat.regions[0])
        setEditProvider(provider)
      })
      .catch((err: unknown) => setError(String(err)))
      .finally(() => setLoading(false))
  }, [provider, onProviderChange])

  function addSpec() {
    if (!editName.trim()) return
    const spec: ResourceSpec = {
      name: editName.trim(),
      type: editType,
      provider: editProvider,
      region: editRegion,
    }
    const next = [...specs, spec]
    setSpecs(next)
    onSpecsChange?.(next)
    setEditName('')
  }

  function removeSpec(idx: number) {
    const next = specs.filter((_, i) => i !== idx)
    setSpecs(next)
    onSpecsChange?.(next)
  }

  const types = catalog?.types ?? []
  const regions = catalog?.regions ?? []

  return (
    <section>
      <h2>Resources</h2>
      <p className="section-desc">
        Live resources from provider. Use the editor to queue specs for planning.
      </p>

      <div className="filter-row">
        <label>
          Provider
          <select value={provider} onChange={(e) => setProvider(e.target.value)}>
            {PROVIDERS.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        </label>
        {catalog && (
          <span className="catalog-source">
            catalog: <em>{catalog.source}</em>
          </span>
        )}
      </div>

      {loading && <p className="loading">Loading resources…</p>}
      {error && <p className="error">{error}</p>}

      {!loading && (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Provider</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {resources.length === 0 ? (
              <tr>
                <td colSpan={4} className="empty">No resources found.</td>
              </tr>
            ) : (
              resources.map((r) => (
                <tr key={`${r.provider}/${r.type}/${r.name}`}>
                  <td>{r.name}</td>
                  <td>{r.type}</td>
                  <td>{r.provider}</td>
                  <td>
                    <span className={`status-badge status-${r.status}`}>{r.status}</span>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}

      <section className="editor-section">
        <h3>Add Resource Spec</h3>
        <p className="section-desc">
          Region and type are populated from the provider catalog — no free-text entry.
        </p>
        <div className="spec-form">
          <label>
            Name
            <input
              type="text"
              placeholder="my-resource"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
            />
          </label>
          <label>
            Provider
            <select value={editProvider} onChange={(e) => setEditProvider(e.target.value)}>
              {PROVIDERS.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </label>
          <label>
            Type
            {types.length > 0 ? (
              <select value={editType} onChange={(e) => setEditType(e.target.value)}>
                {types.map((t) => (
                  <option key={t} value={t}>{t}</option>
                ))}
              </select>
            ) : (
              <select disabled><option>— no catalog —</option></select>
            )}
          </label>
          <label>
            Region
            {regions.length > 0 ? (
              <select value={editRegion} onChange={(e) => setEditRegion(e.target.value)}>
                {regions.map((r) => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
            ) : (
              <select disabled><option>— no catalog —</option></select>
            )}
          </label>
          <button
            className="btn btn-primary"
            onClick={addSpec}
            disabled={!editName.trim() || types.length === 0}
          >
            Add
          </button>
        </div>

        {specs.length > 0 && (
          <>
            <h4 className="queued-heading">Queued Specs ({specs.length})</h4>
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Provider</th>
                  <th>Region</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {specs.map((s, i) => (
                  <tr key={i}>
                    <td>{s.name}</td>
                    <td>{s.type}</td>
                    <td>{s.provider}</td>
                    <td>{s.region}</td>
                    <td>
                      <button className="btn btn-danger btn-sm" onClick={() => removeSpec(i)}>
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </section>
    </section>
  )
}
