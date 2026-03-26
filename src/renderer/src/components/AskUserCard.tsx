import React, { useState, useEffect } from 'react'
import { MessageSquareWarning } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { PendingAskUserRequest } from '@/state/appReducer'

interface AskUserCardProps {
  request: PendingAskUserRequest
  onRespond: (params: {
    cancelled?: boolean
    answers: Array<{ index: number; question: string; answer: string }>
  }) => void
}

export function AskUserCard({ request, onRespond }: AskUserCardProps) {
  const [currentStep, setCurrentStep] = useState(0)
  const [askAnswers, setAskAnswers] = useState<Record<number, string>>({})

  useEffect(() => {
    setCurrentStep(0)
    const next: Record<number, string> = {}
    for (const q of request.questions) next[q.index] = ''
    setAskAnswers(next)
  }, [request.requestKey, request.questions])

  const questions = request.questions
  const totalSteps = questions.length
  const q = questions[currentStep]
  const isLastStep = currentStep >= totalSteps - 1
  const isFirstStep = currentStep === 0

  const handleStepNext = () => {
    if (isLastStep) {
      const out = questions.map((item) => ({
        index: item.index,
        question: item.question,
        answer: String(askAnswers[item.index] || ''),
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
      <div className="mx-auto max-w-3xl rounded-2xl border bg-card overflow-hidden">
        <div className="flex items-center gap-2 px-6 pt-4 pb-2">
          <MessageSquareWarning className="size-5 shrink-0 " />
          <span className="text-sm font-medium text-foreground">Input required</span>
          {totalSteps > 1 && (
            <div className="ml-auto flex items-center gap-2">
              <div className="flex items-center gap-1">
                {questions.map((_, i) => (
                  <div
                    key={i}
                    className={`size-1.5 rounded-full transition-colors duration-200 ${
                      i < currentStep
                        ? 'bg-emerald-500'
                        : i === currentStep
                          ? 'bg-foreground'
                          : 'bg-muted-foreground/30'
                    }`}
                  />
                ))}
              </div>
              <span className="text-xs text-muted-foreground">
                {currentStep + 1}/{totalSteps}
              </span>
            </div>
          )}
        </div>

        {q && (
          <div className="px-6 pb-4 space-y-3">
            <div className="text-sm text-foreground leading-relaxed">
              {q.topic ? (
                <span className="font-medium text-muted-foreground">[{q.topic}] </span>
              ) : null}
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
                        ? 'border-foreground/40 text-foreground'
                        : 'border-border bg-background text-muted-foreground hover:text-foreground hover:bg-accent'
                    }`}
                    onClick={() => setAskAnswers((prev) => ({ ...prev, [q.index]: opt }))}
                  >
                    {opt}
                  </button>
                ))}
              </div>
            )}
            <label className="text-xs font-medium text-muted-foreground">Your answer</label>
            <input
              value={askAnswers[q.index] || ''}
              onChange={(e) => setAskAnswers((prev) => ({ ...prev, [q.index]: e.target.value }))}
              placeholder="Type your answer..."
              className="w-full rounded-lg border border-border bg-background px-4 py-2.5 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleStepNext()
                }
              }}
            />
          </div>
        )}

        <div className="flex items-center justify-end gap-2 px-5 pb-3">
          <Button
            variant="ghost"
            size="sm"
            className="text-xs text-muted-foreground"
            onClick={() => onRespond({ cancelled: true, answers: [] })}
          >
            Cancel
          </Button>
          {!isFirstStep && (
            <Button
              variant="ghost"
              size="sm"
              className="text-xs text-muted-foreground"
              onClick={handleStepBack}
            >
              Back
            </Button>
          )}
          <Button size="sm" className="text-xs" onClick={handleStepNext}>
            {isLastStep ? 'Submit' : 'Next'}
          </Button>
        </div>
      </div>
    </footer>
  )
}
