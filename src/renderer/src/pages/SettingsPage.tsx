import React from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectSeparator, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useCustomModels, useCommitMessageModelId, useLanAccessEnabled, useActions } from '@/store'
import { MODEL_GROUPS } from '@/types'

export function SettingsPage() {
  const navigate = useNavigate()
  const customModels = useCustomModels()
  const commitMessageModelId = useCommitMessageModelId()
  const lanAccessEnabled = useLanAccessEnabled()
  const { setCommitMessageModelId, setLanAccessEnabled } = useActions()

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <div className="mx-auto w-full max-w-2xl space-y-8 p-8">
        <div>
          <h1 className="text-xl font-semibold">Settings</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Configure your Droi environment.
          </p>
        </div>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">API Keys</h2>
            <p className="text-xs text-muted-foreground">
              Manage your Factory API keys. Keys are automatically rotated based on expiry date.
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => navigate({ to: '/settings/keys' })}>
            Manage Keys
          </Button>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">Commit</h2>
            <p className="text-xs text-muted-foreground">
              Base model used to generate commit messages (and PR title/body) when the Commit dialog input is empty.
            </p>
          </div>

          <Select value={commitMessageModelId} onValueChange={(v) => v && setCommitMessageModelId(v)}>
            <SelectTrigger className="w-full justify-between">
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="min-w-[260px]">
              {customModels && customModels.length > 0 && (
                <>
                  <SelectGroup>
                    <SelectLabel className="px-2">Custom</SelectLabel>
                    {customModels.map((m) => (
                      <SelectItem key={m.id} value={m.id}>{m.displayName}</SelectItem>
                    ))}
                  </SelectGroup>
                  <SelectSeparator />
                </>
              )}
              {MODEL_GROUPS.map((group) => (
                <SelectGroup key={group.label}>
                  <SelectLabel className="px-2">{group.label}</SelectLabel>
                  {group.options.map((m) => (
                    <SelectItem key={m.value} value={m.value}>{m.label}</SelectItem>
                  ))}
                </SelectGroup>
              ))}
            </SelectContent>
          </Select>
        </section>

        <Separator />

        <section className="space-y-3">
          <div>
            <h2 className="text-sm font-medium">LAN Access</h2>
            <p className="text-xs text-muted-foreground">
              Allow devices on the same network to access Droi. Requires restart to take effect.
            </p>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
            <Switch
              checked={lanAccessEnabled}
              onCheckedChange={setLanAccessEnabled}
            />
            Enable LAN access
          </label>
        </section>
      </div>
    </div>
  )
}
