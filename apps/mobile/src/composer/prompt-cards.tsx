// Prompts take the composer's place (ADR 0003): an approval is answered with one
// tap; an ask_user question one question at a time, with a line for an answer of
// one's own, or cancelled. Whichever Client answers first wins, and the card goes
// everywhere.
import type { ApprovalRequest, AskRequest } from '@droi/daemon-layer/kernel-wire'
import { scriptToolNames } from '@droi/daemon-layer/script-runs'
import { approvalDetail } from '@droi/daemon-layer/tool-calls'
import { usePromptActions, usePrompts, type AskAnswer } from '@droi/daemon-layer/use-prompts'
import { Check, MessageCircleQuestion, Pencil, ShieldAlert, X } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, TextInput, View } from 'react-native'
import { Button, Text } from '../ui/primitives'
import { useTextScale } from '../ui/text-scale'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { SourceView } from '../transcript/script-run'

/** A Prompt the run waits on, which takes the composer's place; a question that does not block sits above it. */
export function hasPrompt(prompts: ReturnType<typeof usePrompts>): boolean {
  return prompts.permissions.length > 0 || prompts.askUser.some((ask) => ask.blocking !== false)
}

export function PromptArea({ sessionId }: { sessionId: string }) {
  const colors = useColors()
  const prompts = usePrompts(sessionId)
  const actions = usePromptActions(sessionId)
  if (prompts.permissions.length + prompts.askUser.length === 0 && !actions.error) return null
  return (
    <View aria-live="polite" style={styles.area}>
      {actions.error ? (
        <Text role="alert" size="xs" style={{ color: colors.destructiveForeground }}>
          {actions.error}
        </Text>
      ) : null}
      {prompts.permissions.map((permission) => (
        <PermissionCard
          key={permission.requestId}
          permission={permission}
          onAnswer={(option) => void actions.answerPermission(permission, option)}
        />
      ))}
      {prompts.askUser.map((request) => (
        <AskUserCard
          key={request.requestId}
          request={request}
          onAnswer={(answers) => void actions.answerQuestions(request, answers)}
          onCancel={() => void actions.cancelQuestions(request)}
        />
      ))}
    </View>
  )
}

function PermissionCard({
  permission,
  onAnswer,
}: {
  permission: ApprovalRequest
  onAnswer: (optionId: string) => void
}) {
  const colors = useColors()
  const title = permission.tool
  const code = permission.tool === 'codemode' ? permission.args['code'] : undefined
  return (
    <View
      role="group"
      aria-label={`Permission request: ${title}`}
      style={[styles.card, { borderColor: colors.border, backgroundColor: colors.card }]}
    >
      <View style={styles.kicker}>
        <ShieldAlert size={12} color={colors.attention} strokeWidth={2} />
        <Text size="xs" weight="semibold" style={{ color: colors.attention }}>
          Permission
        </Text>
      </View>
      <Text size="sm" weight="medium">
        Droid wants to run {title}
      </Text>
      {typeof code === 'string' ? (
        <ScriptPermissionDetail script={code} />
      ) : (
        <Text selectable size="xs" mono style={[styles.detail, { backgroundColor: colors.muted }]}>
          {approvalDetail(permission.tool, permission.args)}
        </Text>
      )}
      <View style={styles.options}>
        {permission.options.map((option) => {
          const cancel = option.id === 'cancel'
          return (
            <Pressable
              key={option.id}
              role="button"
              aria-label={option.label}
              onPress={() => onAnswer(option.id)}
              style={({ pressed }) => [
                styles.option,
                { backgroundColor: pressed ? colors.accent : colors.card },
              ]}
            >
              <Text
                size="sm"
                weight="medium"
                tone={cancel ? 'muted' : 'default'}
                style={styles.fill}
              >
                {option.label}
              </Text>
              {cancel ? (
                <X size={14} color={colors.mutedForeground} />
              ) : (
                <Check size={14} color={colors.foreground} />
              )}
            </Pressable>
          )
        })}
      </View>
    </View>
  )
}

/** A script asks to run: the tools it calls, with its source a tap away. */
function ScriptPermissionDetail({ script }: { script: string }) {
  const colors = useColors()
  const tools = scriptToolNames(script)
  const [open, setOpen] = useState(tools.length === 0)
  return (
    <View style={[styles.script, { backgroundColor: colors.muted }]}>
      {tools.length > 0 ? (
        <View role="list" aria-label="Tools the script calls" style={styles.scriptCalls}>
          {tools.map((tool) => (
            <View key={tool} role="listitem" style={styles.scriptCall}>
              <Text size="xs" mono>
                {tool}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
      <Pressable
        role="button"
        aria-expanded={open}
        onPress={() => setOpen(!open)}
        style={[styles.sourceToggle, { borderTopColor: colors.border }]}
      >
        <Text tone="muted" size="xs">
          {open ? 'Hide source' : 'Show source'}
        </Text>
      </Pressable>
      {open ? <SourceView script={script} /> : null}
    </View>
  )
}

function AskUserCard({
  request,
  onAnswer,
  onCancel,
}: {
  request: AskRequest
  onAnswer: (answers: AskAnswer[]) => void
  onCancel: () => void
}) {
  const colors = useColors()
  const scale = useTextScale()
  const [step, setStep] = useState(0)
  const [chosen, setChosen] = useState<Record<string, string[]>>({})
  const [custom, setCustom] = useState<Record<string, string>>({})

  const question = request.questions[step]
  if (!question) return null
  const options = question.options ?? []
  const selected = chosen[question.questionId] ?? []
  const typed = custom[question.questionId] ?? ''
  const canContinue = selected.length > 0 || typed.trim().length > 0
  const last = step + 1 === request.questions.length

  const select = (optionId: string) => {
    setCustom((prev) => ({ ...prev, [question.questionId]: '' }))
    setChosen((prev) => {
      const current = prev[question.questionId] ?? []
      if (!question.multiSelect) return { ...prev, [question.questionId]: [optionId] }
      return {
        ...prev,
        [question.questionId]: current.includes(optionId)
          ? current.filter((o) => o !== optionId)
          : [...current, optionId],
      }
    })
  }

  const type = (value: string) => {
    setCustom((prev) => ({ ...prev, [question.questionId]: value }))
    if (value.trim()) setChosen((prev) => ({ ...prev, [question.questionId]: [] }))
  }

  const advance = () => {
    if (!canContinue) return
    if (!last) {
      setStep(step + 1)
      return
    }
    onAnswer(
      request.questions.map((q) => {
        const own = custom[q.questionId]?.trim()
        return own
          ? { questionId: q.questionId, text: own }
          : { questionId: q.questionId, selected: chosen[q.questionId] ?? [] }
      }),
    )
  }

  return (
    <View
      role="group"
      aria-label="Droid has a question"
      style={[styles.card, { borderColor: colors.border, backgroundColor: colors.card }]}
    >
      <View style={styles.kicker}>
        <MessageCircleQuestion size={12} color={colors.mutedForeground} strokeWidth={2} />
        <Text size="xs" weight="semibold" tone="muted" style={styles.fill} numberOfLines={2}>
          {request.context}
        </Text>
        {request.questions.length > 1 ? (
          <Text size="xs" tone="muted" style={[styles.progress, { backgroundColor: colors.muted }]}>
            {step + 1} / {request.questions.length}
          </Text>
        ) : null}
      </View>
      <Text size="sm" weight="medium">
        {question.question}
      </Text>
      {options.length > 0 ? (
        <View
          role={question.multiSelect ? 'group' : 'radiogroup'}
          aria-label={question.question}
          style={styles.options}
        >
          {options.map((option) => {
            const checked = selected.includes(option.id)
            return (
              <Pressable
                key={option.id}
                role={question.multiSelect ? 'checkbox' : 'radio'}
                aria-checked={checked}
                aria-label={option.label}
                accessibilityHint={option.description}
                onPress={() => select(option.id)}
                style={[
                  styles.option,
                  option.description ? styles.described : null,
                  {
                    backgroundColor: checked ? colors.accent : colors.card,
                    borderColor: checked ? colors.ring : 'transparent',
                  },
                ]}
              >
                <View style={styles.fill}>
                  <Text size="sm" weight="medium">
                    {option.label}
                  </Text>
                  {option.description ? (
                    <Text size="xs" tone="muted">
                      {option.description}
                    </Text>
                  ) : null}
                </View>
                {checked ? <Check size={14} color={colors.foreground} /> : null}
              </Pressable>
            )
          })}
        </View>
      ) : null}
      <View style={[styles.own, { backgroundColor: colors.muted }]}>
        <Pencil size={12} color={colors.mutedForeground} />
        <TextInput
          aria-label={`Other answer for: ${question.question}`}
          placeholder="Or type your own answer"
          placeholderTextColor={colors.mutedForeground}
          value={typed}
          onChangeText={type}
          onSubmitEditing={advance}
          style={[
            styles.ownInput,
            { color: colors.foreground, fontSize: styles.ownInput.fontSize * scale },
          ]}
        />
      </View>
      <View style={styles.footer}>
        {step > 0 ? (
          <Button label="Back" variant="ghost" onPress={() => setStep(step - 1)} />
        ) : null}
        <View style={styles.fill} />
        <Button label="Cancel" variant="ghost" onPress={onCancel} />
        <Button label={last ? 'Answer' : 'Next'} disabled={!canContinue} onPress={advance} />
      </View>
    </View>
  )
}

const styles = StyleSheet.create({
  area: { gap: space.sm, paddingHorizontal: space.md },
  card: {
    borderWidth: 1,
    borderRadius: radius.xl,
    padding: space.md,
    gap: space.sm,
  },
  kicker: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  progress: { paddingHorizontal: 6, borderRadius: 5, overflow: 'hidden' },
  detail: { borderRadius: radius.lg, paddingHorizontal: space.sm, paddingVertical: 6 },
  script: { borderRadius: radius.lg, overflow: 'hidden' },
  scriptCalls: { paddingVertical: space.xs },
  scriptCall: {
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: space.sm,
    paddingHorizontal: space.sm,
    paddingVertical: 2,
  },
  sourceToggle: {
    borderTopWidth: 1,
    minHeight: 44,
    justifyContent: 'center',
    paddingHorizontal: space.sm,
    paddingVertical: space.sm,
  },
  options: { gap: space.xs },
  option: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 44,
    paddingHorizontal: space.md,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: 'transparent',
  },
  described: { paddingVertical: space.sm },
  own: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    borderRadius: radius.md,
    paddingHorizontal: space.md,
    minHeight: 44,
  },
  ownInput: { flex: 1, fontFamily: fonts.sans, fontSize: fontSize.base, minHeight: 44 },
  footer: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  fill: { flex: 1 },
})
