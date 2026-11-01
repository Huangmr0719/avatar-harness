interface LamRendererHandle {
  viewer?: { requestFrameId?: number; dispose: () => void | Promise<void> } | null
  dispose: () => void
}

// Renderer 1.4.0's wrapper and viewer both remove the same canvas. Complete
// viewer cleanup first; stop the wrapper's queued frame before its viewer dies.
export async function disposeLamRenderer(renderer: LamRendererHandle, cancel: (id: number) => void = cancelAnimationFrame) {
  const viewer = renderer.viewer
  if (viewer?.requestFrameId != null) cancel(viewer.requestFrameId)
  await viewer?.dispose()
  renderer.dispose()
}
