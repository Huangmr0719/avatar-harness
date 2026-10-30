// Relative RMS used only for small phrase accents; it is not a SER model.
export function speechEnergyCurve(samples: Float32Array, frameCount: number, context: Float32Array): Float32Array {
  const result = new Float32Array(frameCount)
  let square = 0
  for (const value of context) square += value * value
  const reference = Math.max(.02, Math.sqrt(square / Math.max(1, context.length)))
  for (let frame = 0; frame < frameCount; frame++) {
    const begin = Math.floor(frame * samples.length / frameCount), end = Math.floor((frame + 1) * samples.length / frameCount)
    let energy = 0
    for (let i = begin; i < end; i++) energy += samples[i] * samples[i]
    const rms = Math.sqrt(energy / Math.max(1, end - begin))
    result[frame] = rms < .006 ? 0 : Math.max(0, Math.min(1, (rms / reference - .65) / 1.1))
  }
  return result
}
