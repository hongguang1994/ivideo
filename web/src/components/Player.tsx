import { useEffect, useRef } from "react";

// 普通视频和 Safari 原生 HLS 直接使用 <video>；其他浏览器播放 HLS 时才下载 hls.js。
export default function Player({
  src,
  name,
  hls,
  onError,
}: {
  src: string;
  name: string;
  hls?: boolean;
  onError?: (message: string) => void;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const isHls = hls || src.toLowerCase().includes(".m3u8");
    if (!isHls || video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = src;
      video.onerror = () => onError?.("播放器无法加载媒体流，请稍后重试或检查网盘授权。");
      return;
    }

    let disposed = false;
    let destroy: (() => void) | undefined;
    void import("hls.js").then(({ default: Hls }) => {
      if (disposed) return;
      if (!Hls.isSupported()) {
        video.src = src;
        return;
      }
      const player = new Hls();
      destroy = () => player.destroy();
      player.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) onError?.("播放器无法加载媒体流，请稍后重试或检查网盘授权。");
      });
      player.loadSource(src);
      player.attachMedia(video);
    });
    return () => {
      disposed = true;
      destroy?.();
    };
  }, [src, hls, onError]);

  return (
    <video className="player" ref={videoRef} controls autoPlay title={name} />
  );
}
