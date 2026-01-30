import { FileCode2 } from 'lucide-react'

export default function PDDL() {
  return (
    <div className="flex flex-col items-center justify-center h-full bg-[#0f0f1a] text-gray-400">
      <FileCode2 size={64} className="mb-4 text-gray-600" />
      <h1 className="text-2xl font-semibold mb-2">PDDL Editor</h1>
      <p className="text-gray-500">Coming Soon</p>
      <p className="text-gray-600 text-sm mt-2">
        Planning Domain Definition Language 편집기가 이곳에 추가될 예정입니다.
      </p>
    </div>
  )
}
