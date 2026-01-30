import { FC, useState } from 'react'
import {
  EdgeProps,
  getSmoothStepPath,
  EdgeLabelRenderer,
  BaseEdge,
} from 'reactflow'
import { X } from 'lucide-react'

interface DeletableEdgeData {
  outcome?: string
  onDelete?: (edgeId: string) => void
}

const DeletableEdge: FC<EdgeProps<DeletableEdgeData>> = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style = {},
  markerEnd,
  data,
  selected,
}) => {
  const [isHovered, setIsHovered] = useState(false)

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (data?.onDelete) {
      data.onDelete(id)
    }
  }

  // Determine if we should show the delete button
  const showDeleteButton = isHovered || selected

  return (
    <>
      {/* Invisible wider path for easier hover/click detection */}
      <path
        d={edgePath}
        fill="none"
        stroke="transparent"
        strokeWidth={20}
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
        style={{ cursor: 'pointer' }}
      />
      {/* Visible edge path */}
      <BaseEdge
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          ...style,
          strokeWidth: isHovered || selected ? 3 : (style.strokeWidth || 2),
          opacity: isHovered || selected ? 1 : 0.8,
        }}
      />
      {/* Delete button */}
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            pointerEvents: 'all',
          }}
          className="nodrag nopan"
          onMouseEnter={() => setIsHovered(true)}
          onMouseLeave={() => setIsHovered(false)}
        >
          {showDeleteButton && (
            <button
              onClick={handleDelete}
              className="flex items-center justify-center w-5 h-5 rounded-full bg-red-500 hover:bg-red-600 text-white shadow-lg transition-all duration-150 hover:scale-110"
              title="Delete connection"
            >
              <X size={12} />
            </button>
          )}
        </div>
      </EdgeLabelRenderer>
    </>
  )
}

export default DeletableEdge
