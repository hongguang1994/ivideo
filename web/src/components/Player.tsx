import { useEffect, useRef } from "react";
import Hls from "hls.js";

// 播放器：普通视频用原生 <video>；.m3u8 用 hls.js。
export default function Player({ src, name }: { src: string; name: string }) {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const isHls = src.toLowerCase().includes(".m3u8");
    if (isHls && Hls.isSupported()) {
      const hls = new Hls();
      hls.loadSource(src);
      hls.attachMedia(video);
      return () => hls.destroy();
    }
    // 原生播放（含 Safari 原生 HLS）
    video.src = src;
  }, [src]);

  return (
    <video className="player" ref={videoRef} controls autoPlay title={name} />
  );
}
