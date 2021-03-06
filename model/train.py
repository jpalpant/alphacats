import argparse
import glob
import logging
import os
import shutil

import tensorflow.compat.v1.keras.backend as K
from tensorflow.keras.callbacks import (
    EarlyStopping,
    TerminateOnNaN,
    ModelCheckpoint,
)
from tensorflow.keras.layers import (
    Bidirectional,
    Concatenate,
    Dense,
    Dropout,
    Input,
    GRU,
    LeakyReLU,
    Multiply,
    Softmax,
)
from tensorflow.keras.models import Model
from tensorflow.keras.optimizers import Adam
from tensorflow.keras.regularizers import l2
from tensorflow.keras.utils import plot_model
from tensorflow.python.compiler.tensorrt import trt_convert
import matplotlib.pyplot as plt
import numpy as np
import tensorflow.compat.v1 as tf

# These constants must be kept in sync with the Go code.
TF_GRAPH_TAG = "serve"

MAX_HISTORY = 58
N_ACTION_FEATURES = 16
NUM_CARD_TYPES = 11
NUM_CARDS_IN_DECK = 23
MAX_CARDS_IN_DRAW_PILE = 13
MAX_INSERT_POSITIONS = 8
N_OUTPUTS = 2*NUM_CARD_TYPES + MAX_INSERT_POSITIONS + 1


def build_model(history_shape: tuple, hands_shape: tuple, drawpile_shape: tuple, output_mask_shape: tuple, policy_shape: int):
    logging.info("Building model")
    logging.info("History input shape: %s", history_shape)
    history_input = Input(name="history", shape=history_shape)
    logging.info("Hands input shape: %s", hands_shape)
    hands_input = Input(name="hands", shape=hands_shape)
    logging.info("Draw pile input shape: %s", drawpile_shape)
    drawpile_input = Input(name="drawpile", shape=drawpile_shape)
    logging.info("Output mask shape: %s", output_mask_shape)
    output_mask_input = Input(name="output_mask", shape=output_mask_shape)
    logging.info("Policy output shape: %s", policy_shape)

    # The history (GRU) arm of the model.
    history_hidden_1 = Dense(32)(history_input)
    history_relu_1 = LeakyReLU()(history_hidden_1)
    history_hidden_2 = Dense(16)(history_relu_1)
    history_relu_2 = LeakyReLU()(history_hidden_2)
    history_lstm = Bidirectional(GRU(32, return_sequences=False))(history_relu_2)

    # The draw pile (GRU) arm of the model.
    drawpile_hidden_1 = Dense(16)(drawpile_input)
    drawpile_relu_1 = LeakyReLU()(drawpile_hidden_1)
    drawpile_hidden_2 = Dense(16)(drawpile_relu_1)
    drawpile_relu_2 = LeakyReLU()(drawpile_hidden_2)
    drawpile_lstm = Bidirectional(GRU(16, return_sequences=False))(drawpile_relu_2)

    # The hands arm of the model.
    hands_hidden_1 = Dense(32)(hands_input)
    hands_relu_1 = LeakyReLU()(hands_hidden_1)
    hands_hidden_2 = Dense(32)(hands_relu_1)
    hands_relu_2 = LeakyReLU()(hands_hidden_2)

    # Concatenate history, hand, and draw pile.
    # Then send through some dense layers.
    merged_inputs_1 = Concatenate()([history_lstm, drawpile_lstm, hands_relu_2])
    merged_hidden_1 = Dense(128)(merged_inputs_1)
    relu_1 = LeakyReLU()(merged_hidden_1)
    dropout_1 = Dropout(0.2)(relu_1)
    merged_hidden_2 = Dense(128)(dropout_1)
    relu_2 = LeakyReLU()(merged_hidden_2)

    # Policy output head.
    policy_hidden_1 = Dense(policy_shape, activation='linear', kernel_regularizer=l2(0.001))(relu_2)
    policy_masked = Multiply()([policy_hidden_1, output_mask_input])
    policy_output = Softmax(name='policy')(policy_masked)
    # Value output head.
    value_output = Dense(1, activation='tanh', name='value', kernel_regularizer=l2(0.001))(relu_2)

    model = Model(
        inputs=[history_input, hands_input, drawpile_input, output_mask_input],
        outputs=[policy_output, value_output])
    model.compile(
        loss=['categorical_crossentropy', 'mean_squared_error'],
        optimizer=Adam(clipnorm=1.0),
        metrics=['mean_absolute_error'])
    return model


def plot_metrics(history, output):
    plt.figure()
    for metric in ['loss', 'val_loss']:
        epochs = np.arange(len(history.history[metric])) + 1
        plt.plot(epochs, history.history[metric], label=metric)
    plt.xticks(epochs)
    plt.legend()
    plt.title('Training Loss')
    plt.xlabel('Epoch')
    plt.ylabel('MSE')
    plt.savefig(output)


def load_data(filename: str):
    batch = np.load(filename)
    n_samples = len(batch["Y_value"])
    X_history = batch["X_history"].reshape((n_samples, MAX_HISTORY, N_ACTION_FEATURES))
    X_hands = batch["X_hands"].reshape((n_samples, 3*NUM_CARDS_IN_DECK))
    X_drawpile = batch["X_drawpile"].reshape((n_samples, MAX_CARDS_IN_DRAW_PILE, NUM_CARD_TYPES))
    X_output_mask = batch["X_output_mask"].reshape((n_samples, N_OUTPUTS))
    X = {"history": X_history, "hands": X_hands, "drawpile": X_drawpile, "output_mask": X_output_mask}
    Y_policy = batch["Y_policy"].reshape((n_samples, N_OUTPUTS))
    Y_value = batch["Y_value"].reshape((n_samples, 1))
    logging.info("Mean value of all samples: %.4f", Y_value.mean())
    Y = {"policy": Y_policy, "value": Y_value}
    return X, Y


def main():
    parser = argparse.ArgumentParser(description="Run training on a batch of advantages samples")
    parser.add_argument("input", help="Input with training data (npz)")
    parser.add_argument("output", help="Directory to save trained model to")
    parser.add_argument("--validation_split", type=float, default=0.1,
                        help="Fraction of data to hold out for validation / early-stopping")
    parser.add_argument("--initial_weights", help="Load initial weights from saved model")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO)

    config = tf.ConfigProto(
      gpu_options=tf.GPUOptions(allow_growth=True),
    )
    sess = tf.Session(config=config)
    K.set_session(sess)

    X, y = load_data(args.input)

    history_shape = X["history"][0].shape
    hands_shape = X["hands"][0].shape
    drawpile_shape = X["drawpile"][0].shape
    output_mask_shape = X["output_mask"][0].shape
    policy_shape = y["policy"][0].shape[0]
    model = build_model(history_shape, hands_shape, drawpile_shape, output_mask_shape, policy_shape)
    print(model.summary())
    print("Input layer names:", [node.op.name for node in model.inputs])
    print("Output layer names:", [node.op.name for node in model.outputs])

    if args.initial_weights:
        logging.info("Loading initial weights from: %s", args.initial_weights)
        model.load_weights(args.initial_weights)

    # Use batches in largest power of two such that:
    #   1. At least 128 batches per epoch.
    #   2. Minimum batch size 32, max 2048.
    n_samples = len(y["value"])
    batch_size = 32
    while batch_size * 128 < n_samples and batch_size < 2048:
        batch_size *= 2
    logging.info("Using batch size: %d", batch_size)
    history = model.fit(
        x=X,
        y=y,
        batch_size=batch_size,
        epochs=100,
        validation_split=0.1,
        callbacks=[
            EarlyStopping(
                monitor='val_loss', min_delta=0.0001, patience=5,
                restore_best_weights=True),
            TerminateOnNaN(),
        ],
    )

    if os.path.exists(args.output):
        shutil.rmtree(args.output)

    logging.info("Saving model to %s", args.output)
    model.save(os.path.join(args.output, "original"))
    logging.info("Optimizing model with TensorRT")
    params = trt_convert.DEFAULT_TRT_CONVERSION_PARAMS._replace(
            precision_mode="FP16")
    converter = trt_convert.TrtGraphConverterV2(
            input_saved_model_dir=os.path.join(args.output, "original"),
            conversion_params=params)
    converter.convert()
    logging.info("Saving optimized model to %s", args.output)
    converter.save(args.output)
    # Save keras model weights for re-initialization on next iteration.
    model.save_weights(os.path.join(args.output, "weights.h5"))
    plot_model(model, to_file=os.path.join(args.output, 'model.pdf'),
               show_layer_names=False, show_shapes=True)
    plot_metrics(history, os.path.join(args.output, "metrics.pdf"))


if __name__ == "__main__":
  main()
