import { useEffect, useState } from "react";

type Props = {
  src: string;
  poster: string;
  title: string;
};

export function VideoPlayer({ src, poster, title }: Props) {
  const isTranscode = src.includes("/p/transcode/");
  const [playbackSrc, setPlaybackSrc] = useState(isTranscode ? "" : src);
  const [transcodeStatus, setTranscodeStatus] = useState<
    "idle" | "processing" | "error"
  >("idle");

  useEffect(() => {
    if (!isTranscode) {
      setPlaybackSrc(src);
      setTranscodeStatus("idle");
      return;
    }

    let active = true;
    let timer: number | null = null;

    async function poll(start: boolean) {
      try {
        const statusResp = await fetch(`${src}/status`, {
          credentials: "include",
        });
        if (!statusResp.ok) throw new Error("status failed");
        const statusBody = (await statusResp.json()) as { status?: string };
        if (!active) return;

        if (statusBody.status === "ready") {
          setPlaybackSrc(src);
          setTranscodeStatus("idle");
          return;
        }

        if (start) {
          await fetch(`${src}/start`, {
            method: "POST",
            credentials: "include",
          });
        }

        setPlaybackSrc("");
        setTranscodeStatus("processing");
        timer = window.setTimeout(() => poll(false), 3000);
      } catch {
        if (!active) return;
        setPlaybackSrc("");
        setTranscodeStatus("error");
      }
    }

    setPlaybackSrc("");
    setTranscodeStatus("processing");
    void poll(true);

    return () => {
      active = false;
      if (timer) window.clearTimeout(timer);
    };
  }, [isTranscode, src]);

  return (
    <div className="video-player">
      <video
        src={playbackSrc || undefined}
        poster={poster}
        controls
        preload="metadata"
        playsInline
        aria-label={title}
      />
      {isTranscode && !playbackSrc && (
        <div className="video-player__status">
          {transcodeStatus === "error"
            ? "转码启动失败，请稍后重试"
            : "正在准备可快进版本..."}
        </div>
      )}
    </div>
  );
}
