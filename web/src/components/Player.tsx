import { useEffect, useRef } from "react";
import Hls from "hls.js";

// 播放器：普通视频用原生 <video>；HLS 用 hls.js（显式 hls 或 src 含 .m3u8）。
export default function Player({
  src,
  name,
  hls,
}: {
  src: string;
  name: string;
  hls?: boolean;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const isHls = hls || src.toLowerCase().includes(".m3u8");
    if (isHls && Hls.isSupported()) {
      const hls = new Hls();
      hls.loadSource(src);
      hls.attachMedia(video);
      return () => hls.destroy();
    }
    // 原生播放（含 Safari 原生 HLS）
    video.src = src;
  }, [src, hls]);

  return (
    <video className="player" ref={videoRef} controls autoPlay title={name} />
  );
}
