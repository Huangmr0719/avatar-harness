// A single-photo reconstruction may have a smaller usable oral range than the
// audio model's [0,1] output. Preserve small openings; compress high ones smoothly.
// Apply before manual controls so the full rig remains available for inspection.
export function calibrateSpeechJaw(values: Record<string, number>, maximum = 1) {
  if (!Number.isFinite(maximum) || maximum <= 0 || maximum >= 1) return
  const jaw = values.jawOpen
  const knee = maximum * .55, range = maximum - knee
  if (jaw > knee) values.jawOpen = knee + range * (1 - Math.exp(-(jaw - knee) / range))
}
