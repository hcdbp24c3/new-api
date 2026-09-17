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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { previewModelsDevSync, applyModelsDevSync } from '../../api'
import type {
  ModelsDevSyncCandidate,
  ModelsDevSyncPreview,
  ModelsDevSyncSelection,
} from '../../types'

const KIND_LABELS: Record<string, string> = {
  create: 'New',
  update: 'Update',
}

type Step = 'preview' | 'select' | 'results'

export function ModelsDevSyncDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<ModelsDevSyncPreview | null>(null)
  const [step, setStep] = useState<Step>('preview')
  const [selection, setSelection] = useState<Record<string, boolean>>({})
  const [results, setResults] = useState<{
    created: string[]
    updated: string[]
  } | null>(null)

  const load = useMutation({
    onMutate: () => {
      setPreview(null)
      setStep('preview')
    },
    mutationFn: async () => {
      const response = await previewModelsDevSync()
      if (!response.success || !response.data) {
        throw createServerError(response, t('Failed to load models.dev data'))
      }
      return response.data
    },
    onSuccess: (data) => {
      setPreview(data)
      // Auto-select all candidates
      const sel: Record<string, boolean> = {}
      for (const c of data.candidates) {
        sel[c.model_name] = true
      }
      setSelection(sel)
      setStep('select')
    },
    onError: (error) => handleServerError(error),
  })

  const apply = useMutation({
    mutationFn: async (params: {
      source_version: string
      selections: ModelsDevSyncSelection[]
    }) => {
      const response = await applyModelsDevSync(params)
      if (!response.success || !response.data) {
        throw createServerError(response, t('Sync failed'))
      }
      return response.data
    },
    onSuccess: async (data) => {
      await queryClient.invalidateQueries({ queryKey: ['models'] })
      await queryClient.invalidateQueries({ queryKey: ['vendors'] })
      await queryClient.invalidateQueries({ queryKey: ['pricing'] })
      setResults({ created: data.created_models, updated: data.updated_models })
      setStep('results')
    },
    onError: (error) => handleServerError(error),
  })

  const candidates = preview?.candidates ?? []
  const selectedCount = Object.values(selection).filter(Boolean).length

  const columns = useMemo(
    () => [
      {
        id: 'model_name',
        header: t('Model'),
        cell: (row: ModelsDevSyncCandidate) => row.model_name,
      },
      {
        id: 'provider',
        header: t('Provider'),
        cell: (row: ModelsDevSyncCandidate) => row.provider,
      },
      {
        id: 'kind',
        header: t('Type'),
        cell: (row: ModelsDevSyncCandidate) =>
          KIND_LABELS[row.kind] ?? row.kind,
      },
      {
        id: 'fields',
        header: t('Changes'),
        cell: (row: ModelsDevSyncCandidate) =>
          row.fields.map((f) => f.field).join(', '),
      },
      {
        id: 'select',
        header: '',
        cell: (row: ModelsDevSyncCandidate) => (
          <Checkbox
            checked={selection[row.model_name] ?? false}
            onCheckedChange={(checked) =>
              setSelection((prev) => ({
                ...prev,
                [row.model_name]: !!checked,
              }))
            }
          />
        ),
      },
    ],
    [t, selection]
  )

  const handleApply = () => {
    if (!preview) return
    const selections: ModelsDevSyncSelection[] = candidates
      .filter((c) => selection[c.model_name])
      .map((c) => ({
        model_name: c.model_name,
        create: c.kind === 'create',
        fields: c.fields.map((f) => f.field),
      }))
    apply.mutate({
      source_version: preview.source.version,
      selections,
    })
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      setPreview(null)
      setStep('preview')
      setSelection({})
      setResults(null)
      load.reset()
      apply.reset()
    }
    props.onOpenChange(open)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Sync from models.dev')}
      description={t('Fetch model capabilities and pricing from models.dev')}
      footer={
        step === 'select' ? (
          <>
            <Button
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={apply.isPending}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleApply}
              disabled={selectedCount === 0 || apply.isPending}
            >
              {apply.isPending
                ? t('Syncing...')
                : t('Sync {{count}} models', { count: selectedCount })}
            </Button>
          </>
        ) : step === 'results' ? (
          <Button onClick={() => handleOpenChange(false)}>
            {t('Close')}
          </Button>
        ) : (
          <Button
            onClick={() => load.mutate()}
            disabled={load.isPending}
          >
            {load.isPending ? t('Loading...') : t('Load models.dev data')}
          </Button>
        )
      }
    >
      {step === 'preview' && !load.isPending && !load.isError && (
        <EmptyState
          description={t(
            'Click the button below to fetch the latest model data from models.dev'
          )}
        />
      )}

      {load.isPending && <LoadingState />}

      {load.isError && <ErrorState />}

      {step === 'select' && candidates.length === 0 && (
        <EmptyState description={t('No models to sync')} />
      )}

      {step === 'select' && candidates.length > 0 && (
        <div className='space-y-4'>
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {t('{{total}} models found, {{selected}} selected', {
                total: candidates.length,
                selected: selectedCount,
              })}
            </span>
            <div className='flex gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={() => {
                  const sel: Record<string, boolean> = {}
                  for (const c of candidates) sel[c.model_name] = true
                  setSelection(sel)
                }}
              >
                {t('Select All')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setSelection({})}
              >
                {t('Deselect All')}
              </Button>
            </div>
          </div>
          <StaticDataTable
            columns={columns}
            data={candidates}
            getRowKey={(item) => item.model_name}
          />
        </div>
      )}

      {step === 'results' && results && (
        <div className='space-y-4'>
          {results.created.length > 0 && (
            <div>
              <h4 className='font-medium'>{t('Created')}</h4>
              <ul className='text-muted-foreground mt-1 list-inside list-disc text-sm'>
                {results.created.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </div>
          )}
          {results.updated.length > 0 && (
            <div>
              <h4 className='font-medium'>{t('Updated')}</h4>
              <ul className='text-muted-foreground mt-1 list-inside list-disc text-sm'>
                {results.updated.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </div>
          )}
          {results.created.length === 0 && results.updated.length === 0 && (
            <EmptyState description={t('No changes applied')} />
          )}
        </div>
      )}
    </Dialog>
  )
}
