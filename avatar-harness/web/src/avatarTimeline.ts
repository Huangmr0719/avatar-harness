import type { AudioSegment, MotionFrame } from './types.ts'

export class AvatarTimeline {
  private frames: MotionFrame[] = []
  private segments: AudioSegment[] = []
  private cursor = 0
  reset() { this.frames = []; this.segments = []; this.cursor = 0 }
  addFrames(frames: MotionFrame[], basePTS = 0) {
    for (const frame of frames) {
      const pts = frame.pts_ms ?? basePTS + (frame.offset_ms ?? 0)
      if (!Number.isFinite(pts) || pts < 0) continue
      this.frames.push({ ...frame, pts_ms: pts })
    }
    this.frames.sort((a, b) => a.pts_ms! - b.pts_ms!)
  }
  addAudio(segment: AudioSegment) { this.segments.push(segment) }
  sample(now: number): { frame: MotionFrame; audio_pts_ms: number } | null {
    let segment: AudioSegment | undefined
    for (let i = this.segments.length - 1; i >= 0; i--) if (this.segments[i].start_ms <= now) { segment = this.segments[i]; break }
    if (!segment || !this.frames.length) return null
    const audioPTS = segment.pts_ms + Math.min(now - segment.start_ms, segment.duration_ms)
    while (this.cursor + 1 < this.frames.length && this.frames[this.cursor + 1].pts_ms! <= audioPTS) this.cursor++
    const frame = this.frames[this.cursor]
    if (frame.pts_ms! > audioPTS) return null
    return { frame, audio_pts_ms: audioPTS }
  }
}
