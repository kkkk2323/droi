import { useState, type ReactNode } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import { Braces, Check, ChevronRight, CircleAlert, CircleX, Clock, Copy } from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'
import {
  readScriptRun,
  scriptSource,
  scriptSummary,
  type ScriptRun,
} from '@droi/daemon-layer/script-runs'
import { truncateLines } from '@droi/daemon-layer/tool-calls'
import type { ToolCall } from '@droi/daemon-layer/transcript'

const OUTPUT_PREVIEW_LINES = 40

/**
 * A Script, or a WaitForScript picking its run up again, as a row like any
 * tool's, with the calls the run made while it watched listed under it. The
 * row opens to the program, its inputs, and what the model read back.
 */
export function ScriptGroup({ call, children }: { call: ToolCall; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const run = readScriptRun(call)
  const summary = scriptSummary(call)
  const failed = run.status === 'failed' || run.status === 'cancelled'
  const hasCalls = (call.nested?.length ?? 0) > 0

  return (
    <div>
      <Collapsible.Root open={open} onOpenChange={setOpen}>
        <Collapsible.Trigger
          className="group -ml-1.5 flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-left text-[12.5px] outline-none transition-colors hover:bg-muted/70 focus-visible:ring-2 focus-visible:ring-ring/50"
          aria-label={`${call.use.name}: ${summary}`}
        >
          <Braces
            aria-hidden
            className={cn(
              'size-3.5 shrink-0 text-muted-foreground',
              failed && 'text-destructive-foreground',
            )}
          />
          <span className={cn('shrink-0 font-medium', failed && 'text-destructive-foreground')}>
            Script
          </span>
          <span className="min-w-0 flex-1 truncate font-mono text-[11.5px] text-muted-foreground">
            {summary}
          </span>
          <RunStatus call={call} run={run} />
          {call.result ? (
            <ChevronRight
              aria-hidden
              className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-[opacity,transform] duration-150 group-hover:opacity-100 group-focus-visible:opacity-100 group-data-[panel-open]:rotate-90 group-data-[panel-open]:opacity-100"
            />
          ) : null}
        </Collapsible.Trigger>
        <Collapsible.Panel className="mb-1 mt-0.5 overflow-hidden rounded-lg border bg-card/60">
          <ScriptDetail call={call} run={run} />
        </Collapsible.Panel>
      </Collapsible.Root>
      {hasCalls ? (
        <div
          role="group"
          aria-label="Calls made by Script"
          className="ml-[7px] flex flex-col gap-0.5 border-l border-dashed pl-3"
        >
          {children}
        </div>
      ) : null}
    </div>
  )
}

function RunStatus({ call, run }: { call: ToolCall; run: ScriptRun }) {
  if (call.result === null) {
    return <Spinner role="status" aria-label="Running" className="size-3.5 text-muted-foreground" />
  }
  switch (run.status) {
    case 'running':
      // The Script handed back after its first minute; the run goes on.
      return (
        <Clock
          role="img"
          aria-label="Still running"
          className="size-3.5 shrink-0 text-muted-foreground"
        />
      )
    case 'stalled':
      return (
        <CircleAlert role="img" aria-label="Stalled" className="size-3.5 shrink-0 text-attention" />
      )
    case 'failed':
    case 'cancelled':
      return (
        <CircleX
          role="img"
          aria-label={run.status === 'failed' ? 'Failed' : 'Cancelled'}
          className="size-3.5 shrink-0 text-destructive-foreground"
        />
      )
    default:
      return <Check role="img" aria-label="Succeeded" className="size-3.5 shrink-0 text-success" />
  }
}

function ScriptDetail({ call, run }: { call: ToolCall; run: ScriptRun }) {
  const source = scriptSource(call)
  return (
    <div className="flex flex-col divide-y">
      {source ? (
        <Section label="Source">
          <SourceView script={source.script} />
        </Section>
      ) : null}
      {source?.inputs.map((input) => (
        <Section key={input.name} label={`inputs.${input.name}`}>
          <pre className="max-h-60 overflow-auto px-2.5 pb-2 font-mono text-[11.5px] leading-5 whitespace-pre-wrap break-words text-muted-foreground">
            {truncateLines(input.text, OUTPUT_PREVIEW_LINES)}
          </pre>
        </Section>
      ))}
      {run.error || run.output || run.value || run.images.length > 0 ? (
        <Section label="Output">
          <pre className="max-h-96 overflow-auto px-2.5 pb-2 font-mono text-[11.5px] leading-5 whitespace-pre-wrap break-words text-muted-foreground">
            {run.output ? truncateLines(run.output, OUTPUT_PREVIEW_LINES) : null}
            {run.value ? (
              <span className="text-foreground">
                {run.output ? '\n' : ''}
                {truncateLines(run.value, OUTPUT_PREVIEW_LINES)}
              </span>
            ) : null}
            {run.error ? (
              <span className="text-destructive-foreground">
                {run.output || run.value ? '\n' : ''}
                {run.error}
              </span>
            ) : null}
          </pre>
          {run.images.map((src) => (
            <img
              key={src}
              src={src}
              alt="Picture from Script"
              className="mx-2.5 mb-2.5 max-h-80 max-w-[calc(100%-1.25rem)] rounded-md border object-contain object-left"
            />
          ))}
        </Section>
      ) : null}
      {run.stats || run.logPath ? <Footer run={run} /> : null}
    </div>
  )
}

function Section({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section aria-label={label}>
      <h4 className="px-2.5 pt-1.5 pb-1 text-[10.5px] font-medium tracking-wide text-muted-foreground/80 uppercase">
        {label}
      </h4>
      {children}
    </section>
  )
}

function Footer({ run }: { run: ScriptRun }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex min-h-7 items-center gap-2 px-2.5 py-1 text-[11px] text-muted-foreground">
      {run.stats ? <span className="shrink-0 tabular-nums">{run.stats}</span> : null}
      {run.logPath ? (
        <>
          {/* Cut from the left, so the file name stays in view. */}
          <span
            title={run.logPath}
            dir="rtl"
            className="min-w-0 flex-1 truncate text-left font-mono text-[10.5px] text-muted-foreground/70"
          >
            <bdi dir="ltr">{run.logPath}</bdi>
          </span>
          <button
            type="button"
            aria-label={copied ? 'Copied log path' : 'Copy log path'}
            onClick={() => {
              void navigator.clipboard.writeText(run.logPath!).then(() => setCopied(true))
            }}
            className="flex size-5 shrink-0 items-center justify-center rounded outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            {copied ? (
              <Check aria-hidden className="size-3" />
            ) : (
              <Copy aria-hidden className="size-3" />
            )}
          </button>
        </>
      ) : null}
    </div>
  )
}

/** A program with its line numbers, the ones a permission request points at marked. */
export function SourceView({
  script,
  highlight,
  className,
}: {
  script: string
  highlight?: ReadonlySet<number>
  className?: string
}) {
  const lines = script.replace(/\n$/, '').split('\n')
  const width = String(lines.length).length
  return (
    <pre
      aria-label="Script source"
      className={cn('max-h-80 overflow-auto pb-1.5 font-mono text-[11.5px] leading-5', className)}
    >
      {lines.map((line, index) => {
        const number = index + 1
        const marked = highlight?.has(number) === true
        return (
          <span
            key={number}
            data-marked={marked ? '' : undefined}
            className={cn('flex min-w-max', marked && 'bg-attention/10')}
          >
            <span
              aria-hidden
              className={cn(
                'sticky left-0 shrink-0 select-none bg-card pr-2 pl-2.5 text-right text-muted-foreground/60 tabular-nums',
                marked && 'text-attention',
              )}
              style={{ minWidth: `calc(${width}ch + 1.125rem)` }}
            >
              {number}
            </span>
            <span className="whitespace-pre pr-3">{line}</span>
          </span>
        )
      })}
    </pre>
  )
}
