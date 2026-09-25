import { useEffect, useRef, useState } from 'react';
import type { CameraStreamProfile, Device, DeviceAction, EventItem, GlowTone, IconRef, Room, RoomSummary } from '../models/dashboard';
import type { HomeAssistant, HomeAssistantState } from '../ha/types';
import { Icon } from '../components/Icon';
import { StatusBadge } from '../components/StatusBadge';
import { callDeviceAction } from '../ha/services';
import { getRoomImageAsset, getToneClass } from '../utils/assets';

interface RoomHeroProps {
  room: Room;
  roomSummary: RoomSummary;
  roomEvents: EventItem[];
  selectedDevice: Device | null;
  streamProfile: CameraStreamProfile;
  audioMuted: boolean;
  audioVolume: number;
  hass?: HomeAssistant;
}

export function RoomHero({ room, roomSummary, selectedDevice, streamProfile, audioMuted, audioVolume, hass }: RoomHeroProps) {
  const [pendingActionId, setPendingActionId] = useState<string | null>(null);
  const [actionFeedback, setActionFeedback] = useState<string | null>(null);
  const [failedMediaSource, setFailedMediaSource] = useState<string | null>(null);
  const heroMedia = selectedDevice?.heroMedia ?? buildFallbackCameraMedia(selectedDevice);
  const mediaSrc = failedMediaSource === heroMedia?.src
    ? heroMedia.posterSrc ?? heroMedia.src
    : heroMedia?.src;
  const actions = selectedDevice?.actions ?? [];
  const videoMode = heroMedia?.kind === 'stream';
  const showDahuaStats = roomSummary.dahuaCameraCount > 0;
  const climate = roomSummary.climate ?? room.climate;
  const safety = roomSummary.safety ?? room.safety ?? { smokeHigh: 0, coHigh: 0, smokeCapable: 0, coCapable: 0 };
  const hasSmokeSensor = (safety.smokeCapable ?? 0) > 0;
  const hasCoSensor = (safety.coCapable ?? 0) > 0;
  const smokeHigh = hasSmokeSensor ? safety.smokeHigh : 0;
  const coHigh = hasCoSensor ? safety.coHigh : 0;

  async function handleAction(action: DeviceAction) {
    if (!hass?.callService || !selectedDevice || pendingActionId !== null) {
      setActionFeedback('Home Assistant service API unavailable');
      return;
    }

    setPendingActionId(action.id);
    setActionFeedback(null);

    try {
      const sent = await callDeviceAction(hass, selectedDevice, action, (message) => window.confirm(message));
      setActionFeedback(sent ? `Sent ${action.label}` : 'Cancelled');
    } catch {
      setActionFeedback('Action failed');
    } finally {
      setPendingActionId(null);
    }
  }

  return (
    <section
      className={[
        'room-hero',
        heroMedia ? 'room-hero--live' : '',
        videoMode ? 'room-hero--streaming' : '',
        actions.length > 0 ? 'room-hero--actionable' : '',
      ].join(' ')}
      style={!heroMedia ? { backgroundImage: `url(${getRoomImageAsset(room.image)})` } : undefined}
    >
      {heroMedia ? (
        <div className="room-hero__media-wrap">
          {videoMode && hass?.states[heroMedia.entityId] ? (
            <NativeCameraStream
              key={heroMedia.entityId}
              hass={hass}
              stateObj={hass.states[heroMedia.entityId]}
              profile={streamProfile}
              muted={audioMuted}
              volume={audioVolume}
            />
          ) : (
            <img
              key={heroMedia.entityId}
              className="room-hero__media"
              src={mediaSrc ?? heroMedia.src}
              alt={heroMedia.title}
              onError={() => {
                if (heroMedia.posterSrc && mediaSrc !== heroMedia.posterSrc) {
                  setFailedMediaSource(heroMedia.src);
                }
              }}
            />
          )}
        </div>
      ) : null}
      <div className="room-hero__header">
        <div className="room-hero__copy">
          <div className="room-hero__eyebrow">{heroMedia ? `${room.heroLabel} - Live view` : room.heroLabel}</div>
          <h2>{room.name}</h2>
          <p>{room.summary}</p>
          {selectedDevice ? (
            <div className="room-hero__device-meta">
              <StatusBadge label={selectedDevice.name} tone={selectedDevice.isOnline ? 'cyan' : 'red'} />
              {heroMedia ? <StatusBadge label={heroMedia.kind === 'stream' ? 'Live camera' : 'Camera image'} tone="green" /> : null}
            </div>
          ) : null}
        </div>
        <div className="room-hero__stats">
          <RoomHeroStat
            label="SMD 24h"
            value={roomSummary.smdIvs.human + roomSummary.smdIvs.vehicle + roomSummary.smdIvs.animal}
            icon={{ category: 'events', key: 'human_detected' }}
            tone="violet"
          />
          <RoomHeroStat
            label="IVS 24h"
            value={roomSummary.smdIvs.ivs}
            icon={{ category: 'events', key: 'tripwire_detected' }}
            tone="amber"
          />
          {climate?.temperature ? (
            <RoomHeroStat
              label="Temperature"
              value={climate.temperature}
              icon={{ category: 'sensors', key: 'temperature' }}
              tone="cyan"
            />
          ) : null}
          {hasSmokeSensor ? (
            <RoomHeroStat
              label="Hi smoke"
              value={smokeHigh}
              icon={{ category: 'sensors', key: 'smoke' }}
              tone={smokeHigh > 0 ? 'amber' : 'green'}
            />
          ) : null}
          {hasCoSensor ? (
            <RoomHeroStat
              label="Hi CO"
              value={coHigh}
              icon={{ category: 'sensors', key: 'gas' }}
              tone={coHigh > 0 ? 'amber' : 'green'}
            />
          ) : null}
          {showDahuaStats ? (
            <>
              {roomSummary.smdIvs.animal > 0 ? (
                <RoomHeroStat
                  label="Animals 24h"
                  value={roomSummary.smdIvs.animal}
                  icon={{ category: 'events', key: 'motion_detected' }}
                  tone="green"
                />
              ) : null}
            </>
          ) : null}
        </div>
      </div>
      {actions.length > 0 ? (
        <div className="room-hero__action-panel">
          <div className="room-hero__action-copy">
            <span className="room-hero__eyebrow">Device actions</span>
            <strong>{selectedDevice?.name}</strong>
            {actionFeedback ? <span className="room-hero__action-feedback">{actionFeedback}</span> : null}
          </div>
          <div className="room-hero__actions">
            {actions.map((action) => (
              <button
                key={action.id}
                type="button"
                className="room-hero__action-button"
                disabled={pendingActionId !== null || !hass?.callService || action.disabled}
                title={action.disabledReason}
                onClick={() => void handleAction(action)}
              >
                <span>{action.label}</span>
                {action.stateLabel ? <small>{action.stateLabel}</small> : null}
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}

interface NativeCameraStreamProps {
  hass: HomeAssistant;
  stateObj: HomeAssistantState;
  profile: CameraStreamProfile;
  muted: boolean;
  volume: number;
}

type NativeCameraStreamElement = HTMLElement & {
  hass?: HomeAssistant;
  stateObj?: HomeAssistantState;
};

function NativeCameraStream({ hass, stateObj, profile, muted, volume }: NativeCameraStreamProps) {
  const streamRef = useRef<NativeCameraStreamElement | null>(null);

  useEffect(() => {
    const streamElement = streamRef.current;
    if (!streamElement) {
      return;
    }

    const assignStreamProps = () => {
      streamElement.hass = hass;
      streamElement.stateObj = preferFocusedCameraState(stateObj, profile);
    };

    if (customElements.get('ha-camera-stream')) {
      assignStreamProps();
      return;
    }

    let active = true;
    void customElements.whenDefined('ha-camera-stream').then(() => {
      if (active) {
        assignStreamProps();
      }
    });

    return () => {
      active = false;
    };
  }, [hass, profile, stateObj]);

  useEffect(() => {
    const streamElement = streamRef.current;
    if (!streamElement) {
      return;
    }

    return forceNestedVideoObjectFit(streamElement, muted, volume);
  }, [muted, stateObj.entity_id, volume]);

  return (
    <ha-camera-stream
      ref={streamRef}
      className="room-hero__media room-hero__native-stream"
      data-audio-muted={String(muted)}
      data-audio-volume={String(volume)}
    />
  );
}

function forceNestedVideoObjectFit(rootElement: HTMLElement, muted: boolean, volume: number): () => void {
  let pollTimer = 0;
  let active = true;

  const stopPolling = () => {
    if (pollTimer !== 0) {
      window.clearInterval(pollTimer);
      pollTimer = 0;
    }
  };

  const visit = (root: Node & ParentNode, visited = new WeakSet<Node>()): boolean => {
    if (visited.has(root)) {
      return false;
    }
    visited.add(root);

    let foundVideo = false;
    injectCameraFitStyle(root);
    if (root instanceof HTMLElement) {
      configureCameraContainer(root);
      configureCameraPlayerElement(root, muted, volume);
    }

    getDeepElements(root).forEach((element) => {
      if (element instanceof HTMLElement) {
        if (isCameraContainerElement(element)) {
          configureCameraContainer(element);
        }
        configureCameraPlayerElement(element, muted, volume);
      }

      if (element instanceof HTMLVideoElement) {
        foundVideo = true;
        configureCameraVideo(element, muted, volume);
      }

      if (element instanceof HTMLSlotElement) {
        element.assignedElements({ flatten: true }).forEach((assigned) => {
          if (assigned instanceof HTMLElement) {
            foundVideo = visit(assigned, visited) || foundVideo;
          }
        });
      }

      const shadowRoot = element.shadowRoot;
      if (shadowRoot) {
        foundVideo = visit(shadowRoot, visited) || foundVideo;
      }
    });

    return foundVideo;
  };

  function apply() {
    if (!active) {
      return false;
    }

    return visit(rootElement);
  }

  if (!apply()) {
    pollTimer = window.setInterval(() => {
      if (apply()) {
        stopPolling();
      }
    }, 250);
  }

  return () => {
    active = false;
    stopPolling();
  };
}

const CAMERA_FIT_STYLE_ID = 'ajaxbridge-camera-fit-style';
const CAMERA_CONTAINER_SELECTOR = 'ha-camera-stream, ha-web-rtc-player, ha-hls-player, .player, .video, .container';

function getDeepElements(root: Node & ParentNode): Element[] {
  const elements: Element[] = [];
  if (root instanceof Element) {
    elements.push(root);
  }
  root.querySelectorAll('*').forEach((element) => elements.push(element));
  return elements;
}

function isCameraContainerElement(element: HTMLElement): boolean {
  return element.matches(CAMERA_CONTAINER_SELECTOR);
}

function injectCameraFitStyle(root: Node & ParentNode) {
  if (!(root instanceof ShadowRoot) || root.getElementById(CAMERA_FIT_STYLE_ID)) {
    return;
  }

  const style = document.createElement('style');
  style.id = CAMERA_FIT_STYLE_ID;
  style.textContent = `
    video,
    ha-hls-player,
    ha-web-rtc-player,
    ha-camera-stream,
    .player,
    .video,
    .container {
      width: 100% !important;
      height: 100% !important;
      min-width: 100% !important;
      min-height: 100% !important;
      object-fit: fill !important;
    }
  `;
  root.appendChild(style);
}

function configureCameraContainer(element: HTMLElement) {
  element.style.setProperty('display', 'block', 'important');
  element.style.setProperty('width', '100%', 'important');
  element.style.setProperty('height', '100%', 'important');
  element.style.setProperty('min-width', '100%', 'important');
  element.style.setProperty('min-height', '100%', 'important');
  element.style.setProperty('overflow', 'hidden', 'important');
}

function configureCameraPlayerElement(element: HTMLElement, muted: boolean, volume: number) {
  if (!isCameraContainerElement(element)) {
    return;
  }
  const normalizedVolume = Math.min(1, Math.max(0, volume));
  element.toggleAttribute('muted', muted);
  element.setAttribute('autoplay', '');
  element.setAttribute('playsinline', '');
  try {
    const player = element as HTMLElement & {
      muted?: boolean;
      defaultMuted?: boolean;
      volume?: number;
      video?: HTMLVideoElement;
      media?: HTMLVideoElement;
    };
    player.muted = muted;
    player.defaultMuted = muted;
    player.volume = muted ? 0 : normalizedVolume;
    if (player.video instanceof HTMLVideoElement) {
      configureCameraVideo(player.video, muted, volume);
    }
    if (player.media instanceof HTMLVideoElement) {
      configureCameraVideo(player.media, muted, volume);
    }
  } catch {
    // HA custom elements may expose read-only media properties.
  }
}

function configureCameraVideo(video: HTMLVideoElement, muted: boolean, volume: number) {
  const normalizedVolume = Math.min(1, Math.max(0, volume));
  video.autoplay = true;
  video.playsInline = true;
  video.setAttribute('autoplay', '');
  video.setAttribute('playsinline', '');
  video.toggleAttribute('muted', muted);
  video.style.setProperty('object-fit', 'fill', 'important');
  video.style.setProperty('width', '100%', 'important');
  video.style.setProperty('height', '100%', 'important');
  video.style.setProperty('min-width', '100%', 'important');
  video.style.setProperty('min-height', '100%', 'important');
  video.style.setProperty('display', 'block', 'important');
  video.muted = muted;
  video.defaultMuted = muted;
  video.volume = muted ? 0 : normalizedVolume;
}

function preferFocusedCameraState(stateObj: HomeAssistantState, profile: CameraStreamProfile): HomeAssistantState {
  const focusedProfile = resolveFocusedVideoProfile(stateObj.attributes, profile);
  const bridgeProfile = readBridgeProfile(stateObj.attributes, focusedProfile);
  const streamSource = readString(bridgeProfile?.stream_url) || readString(bridgeProfile?.local_stream_url);

  return {
    ...stateObj,
    attributes: {
      ...stateObj.attributes,
      preferred_video_profile: focusedProfile,
      recommended_profile: focusedProfile,
      ...(streamSource ? { stream_source: streamSource } : {}),
    },
  };
}

function resolveFocusedVideoProfile(attributes: Record<string, unknown>, profile: CameraStreamProfile): string {
  const profiles = readRecord(attributes.bridge_profiles);
  if (profile === 'sub') {
    if (profiles?.stable) {
      return 'stable';
    }
    if (profiles?.sub) {
      return 'sub';
    }
  }
  if (profiles?.quality) {
    return 'quality';
  }
  if (profiles?.main) {
    return 'main';
  }

  const preferred = readString(attributes.preferred_video_profile) || readString(attributes.recommended_profile);
  if (preferred && !/stable|sub|low|sd/i.test(preferred)) {
    return preferred;
  }

  return 'quality';
}

function readBridgeProfile(attributes: Record<string, unknown>, profileKey: string): Record<string, unknown> | null {
  const profiles = readRecord(attributes.bridge_profiles);
  return readRecord(profiles?.[profileKey]);
}

function readRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function readString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function buildFallbackCameraMedia(device: Device | null) {
  if (!device || device.type !== 'camera' || !device.entityId.startsWith('camera.')) {
    return undefined;
  }

  return {
    entityId: device.entityId,
    title: device.name,
    kind: 'stream' as const,
    src: `/api/camera_proxy_stream/${device.entityId}`,
    posterSrc: `/api/camera_proxy/${device.entityId}`,
  };
}

interface RoomHeroStatProps {
  label: string;
  value: number | string;
  icon: IconRef;
  tone: GlowTone;
}

function RoomHeroStat({ label, value, icon, tone }: RoomHeroStatProps) {
  return (
    <span className={`room-hero__stat ${getToneClass(tone)}`}>
      <span className="room-hero__stat-icon">
        <Icon icon={icon} size={22} />
      </span>
      <span className="room-hero__stat-copy">
        <strong>{typeof value === 'number' ? formatCount(value) : value}</strong>
        <span>{label}</span>
      </span>
    </span>
  );
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-GB', { maximumFractionDigits: 0 }).format(value);
}
