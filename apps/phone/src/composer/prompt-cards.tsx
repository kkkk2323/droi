// Prompts take the composer's place (ADR 0003): a permission request is
// answered with one tap; an ask-user question one question at a time, with a
// line for an answer of one's own, or cancelled. Whichever Client answers
// first wins, and the card goes everywhere.
import type { PendingAskUserRequest, PendingPermission } from '@factory/droid-sdk'
import { usePromptActions, usePrompts } from '@droi/daemon-layer/use-prompts'
import { Check, MessageCircleQuestion, Pencil, ShieldAlert, X } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, TextInput, View } from 'react-native'
import { Button, Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'

export function hasPrompt(prompts: ReturnType<typeof usePrompts>): boolean {
  return prompts.permissions.length > 0 || prompts.askUser.length > 0
}

export function PromptArea({ sessionId }: { sessionId: string }) {
  const colors = useColors()
  const prompts = usePrompts(sessionId)
  const actions = usePromptActions(sessionId)
  if (!hasPrompt(prompts) && !actions.error) return null
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
  permission: PendingPermission
  onAnswer: (selectedOption: string) => void
}) {
  const colors = useColors()
  const title = permission.toolUses.map((t) => t.toolUse.name).join(', ')
  return (
    <View
      role="group"
      aria-label={`Permission request: ${title}`}
      style={[styles.card, { borderColor: colors.border, backgroundColor: colors.background }]}
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
      {permission.toolUses.map((use) => (
        <Text
          key={use.toolUse.id}
          selectable
          size="xs"
          mono
          style={[styles.detail, { backgroundColor: colors.card }]}
        >
          {toolDetails(use.details, use.toolUse.input)}
        </Text>
      ))}
      <View style={styles.options}>
        {permission.options.map((option) => {
          const cancel = option.value === 'cancel'
          return (
            <Pressable
              key={option.value}
              role="button"
              aria-label={option.label}
              onPress={() => onAnswer(option.value)}
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

function toolDetails(details: unknown, input: Record<string, unknown>): string {
  const d = (details ?? {}) as Record<string, unknown>
  if (typeof d['fullCommand'] === 'string') return `$ ${d['fullCommand']}`
  if (typeof d['filePath'] === 'string') return d['filePath']
  if (typeof input['command'] === 'string') return `$ ${input['command']}`
  return JSON.stringify(input)
}

type Answers = Array<{ index: number; question: string; answer: string }>

function AskUserCard({
  request,
  onAnswer,
  onCancel,
}: {
  request: PendingAskUserRequest
  onAnswer: (answers: Answers) => void
  onCancel: () => void
}) {
  const colors = useColors()
  const [step, setStep] = useState(0)
  const [chosen, setChosen] = useState<Record<number, string[]>>({})
  const [custom, setCustom] = useState<Record<number, string>>({})

  const question = request.questions[step]
  if (!question) return null
  const selected = chosen[question.index] ?? []
  const typed = custom[question.index] ?? ''
  const canContinue = selected.length > 0 || typed.trim().length > 0
  const last = step + 1 === request.questions.length

  const select = (option: string) => {
    setCustom((prev) => ({ ...prev, [question.index]: '' }))
    setChosen((prev) => {
      const current = prev[question.index] ?? []
      if (!question.multiSelect) return { ...prev, [question.index]: [option] }
      return {
        ...prev,
        [question.index]: current.includes(option)
          ? current.filter((o) => o !== option)
          : [...current, option],
      }
    })
  }

  const type = (value: string) => {
    setCustom((prev) => ({ ...prev, [question.index]: value }))
    if (value.trim()) setChosen((prev) => ({ ...prev, [question.index]: [] }))
  }

  const advance = () => {
    if (!canContinue) return
    if (!last) {
      setStep(step + 1)
      return
    }
    onAnswer(
      request.questions.map((q) => {
        const own = custom[q.index]?.trim()
        return {
          index: q.index,
          question: q.question,
          answer: own ? own : (chosen[q.index] ?? []).join(', '),
        }
      }),
    )
  }

  return (
    <View
      role="group"
      aria-label="Droid has a question"
      style={[styles.card, { borderColor: colors.border, backgroundColor: colors.background }]}
    >
      <View style={styles.kicker}>
        <MessageCircleQuestion size={12} color={colors.mutedForeground} strokeWidth={2} />
        <Text size="xs" weight="semibold" tone="muted">
          {question.topic}
        </Text>
        {request.questions.length > 1 ? (
          <Text size="xs" tone="muted" style={[styles.progress, { backgroundColor: colors.card }]}>
            {step + 1} / {request.questions.length}
          </Text>
        ) : null}
      </View>
      <Text size="sm" weight="medium">
        {question.question}
      </Text>
      {question.options.length > 0 ? (
        <View
          role={question.multiSelect ? 'group' : 'radiogroup'}
          aria-label={question.question}
          style={styles.options}
        >
          {question.options.map((option) => {
            const checked = selected.includes(option)
            return (
              <Pressable
                key={option}
                role={question.multiSelect ? 'checkbox' : 'radio'}
                aria-checked={checked}
                aria-label={option}
                onPress={() => select(option)}
                style={[
                  styles.option,
                  {
                    backgroundColor: checked ? colors.accent : colors.card,
                    borderColor: checked ? colors.ring : 'transparent',
                  },
                ]}
              >
                <Text size="sm" weight="medium" style={styles.fill}>
                  {option}
                </Text>
                {checked ? <Check size={14} color={colors.foreground} /> : null}
              </Pressable>
            )
          })}
        </View>
      ) : null}
      <View style={[styles.own, { backgroundColor: colors.card }]}>
        <Pencil size={12} color={colors.mutedForeground} />
        <TextInput
          aria-label={`Other answer for: ${question.question}`}
          placeholder="Or type your own answer"
          placeholderTextColor={colors.mutedForeground}
          value={typed}
          onChangeText={type}
          onSubmitEditing={advance}
          style={[styles.ownInput, { color: colors.foreground }]}
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
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.xl + 4,
    padding: space.md,
    gap: space.sm,
  },
  kicker: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  progress: { paddingHorizontal: 6, borderRadius: 5, overflow: 'hidden' },
  detail: { borderRadius: radius.md, paddingHorizontal: space.sm, paddingVertical: 6 },
  options: { gap: space.xs },
  option: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    minHeight: 40,
    paddingHorizontal: space.md,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: 'transparent',
  },
  own: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    minHeight: 40,
  },
  ownInput: { flex: 1, fontFamily: fonts.sans, fontSize: fontSize.sm, minHeight: 36 },
  footer: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  fill: { flex: 1 },
})
