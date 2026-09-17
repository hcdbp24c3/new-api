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
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const modelDevSyncSchema = z.object({
  'model_dev_sync_setting.enabled': z.boolean(),
  'model_dev_sync_setting.sync_interval_hours': z.coerce.number().min(1).max(168),
})

type ModelDevSyncFormValues = z.infer<typeof modelDevSyncSchema>

type ModelDevSyncSettingsSectionProps = {
  defaultValues: ModelDevSyncFormValues
}

export function ModelDevSyncSettingsSection({
  defaultValues,
}: ModelDevSyncSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm({
    resolver: zodResolver(modelDevSyncSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (data: ModelDevSyncFormValues) => {
    const entries = Object.entries(data) as [string, string | number | boolean][]
    for (const [key, value] of entries) {
      await updateOption.mutateAsync({ key, value: String(value) })
    }
  }

  return (
    <SettingsSection title={t('models.dev Sync')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='model_dev_sync_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable auto sync')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Enable automatic periodic sync from models.dev'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='model_dev_sync_setting.sync_interval_hours'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Sync Interval (hours)')}</FormLabel>
                  <FormDescription>
                    {t(
                      'How often to sync from models.dev (1-168 hours)'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={168}
                    className='w-24'
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
