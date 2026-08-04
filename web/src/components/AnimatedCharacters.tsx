import { useEffect, useRef, useState } from 'react';
import type { CSSProperties, RefObject } from 'react';

/**
 * 会跟着鼠标转眼珠的登录页小家伙们。
 * 移植自 Keduoli03/animatedlogin-react(MIT License, Copyright (c) 2026 Lanke),
 * 配色改为 PocketDrive 的赤陶/暖炭系,逻辑保持原版:
 * - 平时眼睛跟随鼠标,随机眨眼
 * - 输入用户名时互相对视
 * - 输入密码(密文)时集体扭头回避
 * - 显示密码明文时……偶尔偷瞄一眼
 */

const PUPIL = '#2d2a24';

/** 没有眼白的裸眼珠(圆顶小家伙用),自由平移不裁切 */
function Pupil({
    mouseX,
    mouseY,
    size = 12,
    maxDistance = 5,
    forceLookX,
    forceLookY,
}: {
    mouseX: number;
    mouseY: number;
    size?: number;
    maxDistance?: number;
    forceLookX?: number;
    forceLookY?: number;
}) {
    const ref = useRef<HTMLDivElement>(null);
    const [pos, setPos] = useState({ x: 0, y: 0 });

    useEffect(() => {
        if (!ref.current) return;
        if (forceLookX !== undefined && forceLookY !== undefined) {
            setPos({ x: forceLookX, y: forceLookY });
            return;
        }
        const rect = ref.current.getBoundingClientRect();
        const dx = mouseX - (rect.left + rect.width / 2);
        const dy = mouseY - (rect.top + rect.height / 2);
        const dist = Math.min(Math.hypot(dx, dy), maxDistance);
        const angle = Math.atan2(dy, dx);
        setPos({ x: Math.cos(angle) * dist, y: Math.sin(angle) * dist });
    }, [mouseX, mouseY, forceLookX, forceLookY, maxDistance]);

    return (
        <div
            ref={ref}
            style={{
                width: size,
                height: size,
                backgroundColor: PUPIL,
                borderRadius: '50%',
                transform: `translate(${pos.x}px, ${pos.y}px)`,
                transition: 'transform 0.1s ease-out',
            }}
        />
    );
}

function EyeBall({
    mouseX,
    mouseY,
    size = 18,
    pupilSize = 7,
    maxDistance = 5,
    isBlinking = false,
    forceLookX,
    forceLookY,
}: {
    mouseX: number;
    mouseY: number;
    size?: number;
    pupilSize?: number;
    maxDistance?: number;
    isBlinking?: boolean;
    forceLookX?: number;
    forceLookY?: number;
}) {
    const eyeRef = useRef<HTMLDivElement>(null);
    const [pos, setPos] = useState({ x: 0, y: 0 });

    useEffect(() => {
        if (!eyeRef.current) return;
        if (forceLookX !== undefined && forceLookY !== undefined) {
            setPos({ x: forceLookX, y: forceLookY });
            return;
        }
        const eye = eyeRef.current.getBoundingClientRect();
        const dx = mouseX - (eye.left + eye.width / 2);
        const dy = mouseY - (eye.top + eye.height / 2);
        const dist = Math.min(Math.hypot(dx, dy), maxDistance);
        const angle = Math.atan2(dy, dx);
        setPos({ x: Math.cos(angle) * dist, y: Math.sin(angle) * dist });
    }, [mouseX, mouseY, forceLookX, forceLookY, maxDistance]);

    return (
        <div
            ref={eyeRef}
            style={{
                width: size,
                height: isBlinking ? 2 : size,
                backgroundColor: 'white',
                borderRadius: '50%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                overflow: 'hidden',
                transition: 'height 0.15s ease',
            }}
        >
            {!isBlinking && (
                <div
                    style={{
                        width: pupilSize,
                        height: pupilSize,
                        backgroundColor: PUPIL,
                        borderRadius: '50%',
                        transform: `translate(${pos.x}px, ${pos.y}px)`,
                        transition: 'transform 0.1s ease-out',
                    }}
                />
            )}
        </div>
    );
}

function useBlink(): boolean {
    const [blinking, setBlinking] = useState(false);
    useEffect(() => {
        let alive = true;
        let timer: number;
        const run = () => {
            timer = window.setTimeout(() => {
                if (!alive) return;
                setBlinking(true);
                window.setTimeout(() => {
                    if (!alive) return;
                    setBlinking(false);
                    run();
                }, 150);
            }, Math.random() * 4000 + 3000);
        };
        run();
        return () => {
            alive = false;
            window.clearTimeout(timer);
        };
    }, []);
    return blinking;
}

export default function AnimatedCharacters({
    isTyping = false,
    isPasswordFocused = false,
    showPassword = false,
    passwordLength = 0,
}: {
    isTyping?: boolean;
    isPasswordFocused?: boolean;
    showPassword?: boolean;
    passwordLength?: number;
}) {
    const [mouseX, setMouseX] = useState(0);
    const [mouseY, setMouseY] = useState(0);
    const tallBlinking = useBlink();
    const darkBlinking = useBlink();
    const [lookingAtEachOther, setLookingAtEachOther] = useState(false);
    const [peeking, setPeeking] = useState(false);

    const tallRef = useRef<HTMLDivElement>(null);
    const darkRef = useRef<HTMLDivElement>(null);
    const sandRef = useRef<HTMLDivElement>(null);
    const oliveRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const onMove = (e: MouseEvent) => {
            setMouseX(e.clientX);
            setMouseY(e.clientY);
        };
        window.addEventListener('mousemove', onMove);
        return () => window.removeEventListener('mousemove', onMove);
    }, []);

    useEffect(() => {
        if (isTyping) {
            setLookingAtEachOther(true);
            const t = setTimeout(() => setLookingAtEachOther(false), 800);
            return () => clearTimeout(t);
        }
        setLookingAtEachOther(false);
    }, [isTyping]);

    useEffect(() => {
        if (passwordLength > 0 && showPassword) {
            const t = setTimeout(() => {
                setPeeking(true);
                setTimeout(() => setPeeking(false), 800);
            }, Math.random() * 3000 + 2000);
            return () => clearTimeout(t);
        }
        setPeeking(false);
    }, [passwordLength, showPassword]);

    const calcPos = (ref: RefObject<HTMLDivElement | null>) => {
        if (!ref.current) return { faceX: 0, faceY: 0, bodySkew: 0 };
        const rect = ref.current.getBoundingClientRect();
        const dx = mouseX - (rect.left + rect.width / 2);
        const dy = mouseY - (rect.top + rect.height / 3);
        return {
            faceX: Math.max(-15, Math.min(15, dx / 20)),
            faceY: Math.max(-10, Math.min(10, dy / 30)),
            bodySkew: Math.max(-6, Math.min(6, -dx / 120)),
        };
    };

    const tallPos = calcPos(tallRef);
    const darkPos = calcPos(darkRef);
    const sandPos = calcPos(sandRef);
    const olivePos = calcPos(oliveRef);

    const hidingPassword = passwordLength > 0 && !showPassword;
    const lookingAway = isPasswordFocused && !showPassword;
    const staring = passwordLength > 0 && showPassword;

    const charStyle = (
        backgroundColor: string,
        zIndex: number,
        pos: { bodySkew: number },
        dimensions: CSSProperties,
        transform?: string,
    ): CSSProperties => ({
        position: 'absolute',
        backgroundColor,
        zIndex,
        transformOrigin: 'bottom center',
        willChange: 'transform',
        backfaceVisibility: 'hidden',
        transition: `${
            isPasswordFocused || isTyping ? 'transform 0.6s ease-out' : 'transform 0.1s ease-out'
        }, height 0.6s ease-in-out`,
        transform: transform || `skewX(${pos.bodySkew || 0}deg) translateZ(0)`,
        bottom: -2,
        borderBottom: `4px solid ${backgroundColor}`,
        ...dimensions,
    });

    return (
        <div style={{ position: 'relative', width: 550, height: 400, overflow: 'hidden' }}>
            {/* 赤陶高个子 */}
            <div
                ref={tallRef}
                style={charStyle(
                    '#c96442',
                    1,
                    tallPos,
                    {
                        left: 70,
                        width: 180,
                        height: lookingAway || isTyping || hidingPassword ? 440 : 400,
                        borderRadius: '10px 10px 0 0',
                    },
                    staring
                        ? 'skewX(0deg) translateZ(0)'
                        : lookingAway
                          ? 'skewX(-14deg) translateX(-20px) translateZ(0)'
                          : isTyping || hidingPassword
                            ? `skewX(${(tallPos.bodySkew || 0) - 12}deg) translateX(40px) translateZ(0)`
                            : `skewX(${tallPos.bodySkew || 0}deg) translateZ(0)`,
                )}
            >
                <div
                    style={{
                        position: 'absolute',
                        display: 'flex',
                        gap: 32,
                        left: lookingAway
                            ? 20
                            : staring
                              ? 20
                              : lookingAtEachOther
                                ? 55
                                : 45 + tallPos.faceX,
                        top: lookingAway
                            ? 25
                            : staring
                              ? 35
                              : lookingAtEachOther
                                ? 65
                                : 40 + tallPos.faceY,
                        transition: 'all 0.6s ease-out',
                    }}
                >
                    {[0, 1].map((i) => (
                        <EyeBall
                            key={i}
                            mouseX={mouseX}
                            mouseY={mouseY}
                            isBlinking={tallBlinking}
                            forceLookX={
                                lookingAway
                                    ? -5
                                    : staring
                                      ? peeking
                                          ? 4
                                          : -4
                                      : lookingAtEachOther
                                        ? 3
                                        : undefined
                            }
                            forceLookY={
                                lookingAway
                                    ? -5
                                    : staring
                                      ? peeking
                                          ? 5
                                          : -4
                                      : lookingAtEachOther
                                        ? 4
                                        : undefined
                            }
                        />
                    ))}
                </div>
            </div>

            {/* 暖炭小方块 */}
            <div
                ref={darkRef}
                style={charStyle(
                    '#35322c',
                    2,
                    darkPos,
                    { left: 240, width: 120, height: 310, borderRadius: '8px 8px 0 0' },
                    staring
                        ? 'skewX(0deg) translateZ(0)'
                        : lookingAway
                          ? 'skewX(12deg) translateX(-10px) translateZ(0)'
                          : lookingAtEachOther
                            ? `skewX(${(darkPos.bodySkew || 0) * 1.5 + 10}deg) translateX(20px) translateZ(0)`
                            : `skewX(${(darkPos.bodySkew || 0) * 1.5}deg) translateZ(0)`,
                )}
            >
                <div
                    style={{
                        position: 'absolute',
                        display: 'flex',
                        gap: 24,
                        left: lookingAway
                            ? 10
                            : staring
                              ? 10
                              : lookingAtEachOther
                                ? 32
                                : 26 + darkPos.faceX,
                        top: lookingAway
                            ? 20
                            : staring
                              ? 28
                              : lookingAtEachOther
                                ? 12
                                : 32 + darkPos.faceY,
                        transition: 'all 0.6s ease-out',
                    }}
                >
                    {[0, 1].map((i) => (
                        <EyeBall
                            key={i}
                            mouseX={mouseX}
                            mouseY={mouseY}
                            size={16}
                            pupilSize={6}
                            isBlinking={darkBlinking}
                            forceLookX={
                                lookingAway ? -4 : staring ? -4 : lookingAtEachOther ? 0 : undefined
                            }
                            forceLookY={
                                lookingAway
                                    ? -5
                                    : staring
                                      ? -4
                                      : lookingAtEachOther
                                        ? -4
                                        : undefined
                            }
                        />
                    ))}
                </div>
            </div>

            {/* 沙色圆顶 */}
            <div
                ref={sandRef}
                style={charStyle(
                    '#e4b15e',
                    3,
                    sandPos,
                    { left: 0, width: 240, height: 200, borderRadius: '120px 120px 0 0' },
                    staring
                        ? 'skewX(0deg) translateZ(0)'
                        : `skewX(${sandPos.bodySkew || 0}deg) translateZ(0)`,
                )}
            >
                <div
                    style={{
                        position: 'absolute',
                        display: 'flex',
                        gap: 32,
                        left: lookingAway ? 50 : staring ? 50 : 82 + (sandPos.faceX || 0),
                        top: lookingAway ? 75 : staring ? 85 : 90 + (sandPos.faceY || 0),
                        transition: 'all 0.2s ease-out',
                    }}
                >
                    {[0, 1].map((i) => (
                        <Pupil
                            key={i}
                            mouseX={mouseX}
                            mouseY={mouseY}
                            forceLookX={lookingAway ? -5 : staring ? -5 : undefined}
                            forceLookY={lookingAway ? -5 : staring ? -4 : undefined}
                        />
                    ))}
                </div>
            </div>

            {/* 橄榄绿圆顶(有嘴巴) */}
            <div
                ref={oliveRef}
                style={charStyle(
                    '#8a9a6b',
                    4,
                    olivePos,
                    { left: 310, width: 140, height: 230, borderRadius: '70px 70px 0 0' },
                    staring
                        ? 'skewX(0deg) translateZ(0)'
                        : `skewX(${olivePos.bodySkew || 0}deg) translateZ(0)`,
                )}
            >
                <div
                    style={{
                        position: 'absolute',
                        display: 'flex',
                        gap: 24,
                        left: lookingAway ? 20 : staring ? 20 : 52 + (olivePos.faceX || 0),
                        top: lookingAway ? 30 : staring ? 35 : 40 + (olivePos.faceY || 0),
                        transition: 'all 0.2s ease-out',
                    }}
                >
                    {[0, 1].map((i) => (
                        <Pupil
                            key={i}
                            mouseX={mouseX}
                            mouseY={mouseY}
                            forceLookX={lookingAway ? -5 : staring ? -5 : undefined}
                            forceLookY={lookingAway ? -5 : staring ? -4 : undefined}
                        />
                    ))}
                </div>
                <div
                    style={{
                        position: 'absolute',
                        width: 80,
                        height: 4,
                        backgroundColor: PUPIL,
                        borderRadius: 999,
                        left: lookingAway ? 15 : staring ? 10 : 40 + (olivePos.faceX || 0),
                        top: lookingAway ? 78 : staring ? 88 : 88 + (olivePos.faceY || 0),
                        transition: 'all 0.2s ease-out',
                    }}
                />
            </div>
        </div>
    );
}
