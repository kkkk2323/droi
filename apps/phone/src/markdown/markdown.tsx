// Droi's Markdown renderer for React Native: the syntax tree from parse.ts
// drawn with Text and View, in the web Client's type scale. Inline nodes nest
// inside one Text so lines wrap as prose; blocks stack as Views.
import type { Nodes, Parents, PhrasingContent, RootContent, Table } from 'mdast'
import { Check, Copy } from 'lucide-react-native'
import { useState, type ReactNode } from 'react'
import { Linking, Pressable, ScrollView, StyleSheet, Text as RNText, View } from 'react-native'
import { copyText } from '../platform/clipboard'
import { Text } from '../ui/primitives'
import { fontSize, fonts, radius, space, type Colors } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { parseMarkdown } from './parse'

export function Markdown({
  text,
  streaming = false,
  muted = false,
}: {
  text: string
  streaming?: boolean
  muted?: boolean
}) {
  const colors = useColors()
  const tree = parseMarkdown(text, { streaming })
  const context: Context = { colors, muted }
  return (
    <View style={styles.blocks}>{tree.children.map((node, i) => block(node, i, context))}</View>
  )
}

interface Context {
  colors: Colors
  muted: boolean
}

function block(node: RootContent, key: number, context: Context): ReactNode {
  const { colors } = context
  switch (node.type) {
    case 'paragraph':
      return (
        <Prose key={key} context={context}>
          {inline(node.children, context)}
        </Prose>
      )
    case 'heading':
      return (
        <Prose
          key={key}
          context={context}
          role="heading"
          style={{
            fontFamily: fonts.sansSemiBold,
            fontSize:
              node.depth <= 1 ? fontSize.xl : node.depth === 2 ? fontSize.lg : fontSize.base,
            marginTop: space.xs,
          }}
        >
          {inline(node.children, context)}
        </Prose>
      )
    case 'code':
      return <CodeBlock key={key} code={node.value} lang={node.lang ?? null} />
    case 'blockquote':
      return (
        <View key={key} style={[styles.quote, { borderLeftColor: colors.border }]}>
          {node.children.map((child, i) => block(child, i, { ...context, muted: true }))}
        </View>
      )
    case 'list':
      return (
        <View key={key} role="list" style={styles.list}>
          {node.children.map((item, i) => (
            <View key={keyOf(item, i)} role="listitem" style={styles.listItem}>
              <Text tone="muted" style={styles.bullet}>
                {item.checked === true
                  ? '☑'
                  : item.checked === false
                    ? '☐'
                    : node.ordered
                      ? `${(node.start ?? 1) + i}.`
                      : '•'}
              </Text>
              <View style={styles.listBody}>
                {item.children.map((child, j) => block(child, j, context))}
              </View>
            </View>
          ))}
        </View>
      )
    case 'table':
      return <MarkdownTable key={key} table={node} context={context} />
    case 'thematicBreak':
      return <View key={key} style={[styles.rule, { backgroundColor: colors.border }]} />
    case 'html':
      return (
        <Prose key={key} context={context}>
          {node.value}
        </Prose>
      )
    default:
      return 'children' in node ? (
        <Prose key={key} context={context}>
          {inline((node as Parents).children as PhrasingContent[], context)}
        </Prose>
      ) : null
  }
}

function inline(nodes: readonly PhrasingContent[], context: Context): ReactNode[] {
  const { colors } = context
  return nodes.map((node, i) => {
    switch (node.type) {
      case 'text':
        return node.value
      case 'strong':
        return (
          <RNText key={keyOf(node, i)} style={{ fontFamily: fonts.sansSemiBold }}>
            {inline(node.children, context)}
          </RNText>
        )
      case 'emphasis':
        return (
          <RNText key={keyOf(node, i)} style={{ fontStyle: 'italic' }}>
            {inline(node.children, context)}
          </RNText>
        )
      case 'delete':
        return (
          <RNText key={keyOf(node, i)} style={{ textDecorationLine: 'line-through' }}>
            {inline(node.children, context)}
          </RNText>
        )
      case 'inlineCode':
        return (
          <RNText
            key={keyOf(node, i)}
            style={{
              fontFamily: fonts.mono,
              fontSize: fontSize.sm,
              color: colors.code,
              backgroundColor: colors.codeBackground,
            }}
          >
            {node.value}
          </RNText>
        )
      case 'link':
        return (
          <RNText
            key={keyOf(node, i)}
            role="link"
            onPress={() => void Linking.openURL(node.url)}
            style={{ textDecorationLine: 'underline' }}
          >
            {inline(node.children, context)}
          </RNText>
        )
      case 'break':
        return '\n'
      case 'image':
        return node.alt ? `[${node.alt}]` : ''
      case 'html':
        return node.value
      default:
        return 'children' in node
          ? inline((node as Parents).children as PhrasingContent[], context)
          : null
    }
  })
}

/** Siblings start at different offsets in the source; the index is a fallback. */
function keyOf(node: Nodes, index: number): string {
  return String(node.position?.start.offset ?? index)
}

function Prose({
  children,
  context,
  style,
  role,
}: {
  children: ReactNode
  context: Context
  style?: object
  role?: 'heading'
}) {
  return (
    <Text
      selectable
      role={role}
      tone={context.muted ? 'muted' : 'default'}
      style={[styles.prose, style]}
    >
      {children}
    </Text>
  )
}

function CodeBlock({ code, lang }: { code: string; lang: string | null }) {
  const colors = useColors()
  const [copied, setCopied] = useState(false)
  return (
    <View
      role="group"
      aria-label={lang ? `${lang} code` : 'Code'}
      style={[styles.code, { backgroundColor: colors.codeBackground, borderColor: colors.border }]}
    >
      <View style={styles.codeHeader}>
        <Text tone="muted" size="xs" mono>
          {lang ?? ''}
        </Text>
        <Pressable
          role="button"
          aria-label={copied ? 'Copied' : 'Copy code'}
          hitSlop={8}
          onPress={() =>
            void copyText(code).then(() => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1_500)
            })
          }
        >
          {copied ? (
            <Check size={14} color={colors.success} strokeWidth={2} />
          ) : (
            <Copy size={14} color={colors.mutedForeground} strokeWidth={1.75} />
          )}
        </Pressable>
      </View>
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <Text selectable mono size="xs" style={styles.codeText}>
          {code}
        </Text>
      </ScrollView>
    </View>
  )
}

function MarkdownTable({ table, context }: { table: Table; context: Context }) {
  const { colors } = context
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <View role="table" style={[styles.table, { borderColor: colors.border }]}>
        {table.children.map((row, r) => (
          <View
            key={keyOf(row, r)}
            role="row"
            style={[
              styles.tableRow,
              r > 0
                ? { borderTopColor: colors.border, borderTopWidth: StyleSheet.hairlineWidth }
                : null,
            ]}
          >
            {row.children.map((cell, c) => (
              <View
                key={keyOf(cell, c)}
                role={r === 0 ? 'columnheader' : 'cell'}
                style={styles.tableCell}
              >
                <Text size="sm" weight={r === 0 ? 'semibold' : 'regular'}>
                  {inline(cell.children, context)}
                </Text>
              </View>
            ))}
          </View>
        ))}
      </View>
    </ScrollView>
  )
}

const styles = StyleSheet.create({
  blocks: { gap: space.sm },
  prose: { lineHeight: 22 },
  quote: { borderLeftWidth: 2, paddingLeft: space.md, gap: space.sm },
  list: { gap: space.xs },
  listItem: { flexDirection: 'row', gap: space.sm },
  bullet: { minWidth: 14, lineHeight: 22 },
  listBody: { flex: 1, gap: space.xs },
  rule: { height: StyleSheet.hairlineWidth, marginVertical: space.sm },
  code: { borderRadius: radius.lg, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
  codeHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: space.md,
    paddingTop: space.sm,
  },
  codeText: { padding: space.md, lineHeight: 18 },
  table: { borderWidth: StyleSheet.hairlineWidth, borderRadius: radius.md },
  tableRow: { flexDirection: 'row' },
  tableCell: {
    minWidth: 80,
    maxWidth: 240,
    paddingHorizontal: space.sm,
    paddingVertical: space.xs,
  },
})
