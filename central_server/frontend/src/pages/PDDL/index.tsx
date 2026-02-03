import { FileCode2 } from 'lucide-react'

export default function PDDL() {
  return (
    <div className="flex flex-col items-center justify-center h-full bg-base text-secondary">
      <FileCode2 size={64} className="mb-4 text-muted" />
      <h1 className="text-2xl font-semibold mb-2">PDDL Editor</h1>
      <p className="text-muted">Coming Soon</p>
      <p className="text-muted text-sm mt-2">
        Planning Domain Definition Language 편집기가 이곳에 추가될 예정입니다.
      </p>
    </div>
  )
}
