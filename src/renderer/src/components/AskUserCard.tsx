import React, { useState, useEffect } from 'react'
import { MessageSquareWarning } from 'lucide-react'
import type { PendingAskUserRequest } from '@/state/appReducer'

interface AskUserCardProps {
  request: PendingAskUserRequest
  onRespond: (params: { cancelled?: boolean; answers: Array<{ index: number; question: string; answer: string }> }) => void
}

export function AskUserCard({ request, onRespond }: AskUserCardProps) {
  const [currentStep, setCurrentStep] = useState(0)
  const [askAnswers, setAskAnswers] = useState<Record<number, string>>({})

  useEffect(() => {
    setCurrentStep(0)
    const next: Record<number, string> = {}
    for (const q of request.questions) next[q.index] = ''
    setAskAnswers(next)
  }, [request.requestId])

  const questions = request.questions
  const totalSteps = questions.length
  const q = questions[currentStep]
  const isLastStep = currentStep >= totalSteps - 1
  const isFirstStep = currentStep === 0

  const handleStepNext = () => {
    if (isLastStep) {
      const out = questions.map((q) => ({
        index: q.index,
        question: q.question,
        answer: String(askAnswers[q.index] || ''),
      }))
      onRespond({ cancelled: false, answers: out })
    } else {
      setCurrentStep((s) => s + 1)
    }
  }

  const handleStepBack = () => {
    if (!isFirstStep) setCurrentStep((s) => s - 1)
  }

  return (
    <footer className="shrink-0 px-4 pb-4">
      <div className="mx-auto max-w-4xl rounded-2xl border bg-card overflow-hidden">
        <div className="flex items-center gap-2 px-6 pt-4 pb-2">
          <MessageSquareWarning className="size-5 shrink-0 " />
          <span className="text-sm font-medium text-foreground">Input required</span>
          {totalSteps > 1 && (
            <span className="ml-auto text-sm text-muted-foreground">
              Step {currentStep + 1} of {totalSteps}
            </span>
          )}
        </div>

        {q && (
          <div className="px-6 pb-4 space-y-3">
            <div className="text-sm text-foreground leading-relaxed">
              {q.topic ? <span className="font-medium text-muted-foreground">[{q.topic}] </span> : null}
              {q.question}
            </div>
            {q.options.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {q.options.slice(0, 12).map((opt) => (
                  <button
                    key={opt}
                    type="button"
                    className={`rounded-lg border px-4 py-2 text-sm transition-colors ${
                      askAnswers[q.index] === opt
                        ? 'border-blue-400  text-blue-600'
                        : 'border-border bg-background text-muted-foreground hover:text-foreground hover:bg-accent'
                    }`}
                    onClick={() => setAskAnswers((prev) => ({ ...prev, [q.index]: opt }))}
                  >
                    {opt}
                  </button>
                ))}
              </div>
            )}
            <input
              value={askAnswers[q.index] || ''}
              onChange={(e) => setAskAnswers((prev) => ({ ...prev, [q.index]: e.target.value }))}
              placeholder="Type your answer..."
              className="w-full rounded-lg border border-border bg-background px-4 py-2.5 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-blue-400"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleStepNext()
                }
              }}
            />
          </div>
        )}

        <div className="flex items-center justify-end gap-3 px-5 pb-3">
          <button
            type="button"
            onClick={() => onRespond({ cancelled: true, answers: [] })}
            className="rounded-lg px-4 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            Cancel
          </button>
          {!isFirstStep && (
            <button
              type="button"
              onClick={handleStepBack}
              className="rounded-lg px-4 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              Back
            </button>
          )}
          <button
            type="button"
            onClick={handleStepNext}
            className="rounded-lg bg-foreground px-4 py-2 text-sm text-background transition-colors hover:bg-foreground/80"
          >
            {isLastStep ? 'Submit' : 'Next'}
          </button>
        </div>
      </div>
    </footer>
  )
}
